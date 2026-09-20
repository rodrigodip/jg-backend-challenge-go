//go:build integration

package tests

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/jg-backend-challenge/wallet/internal/app"
)

// Fx composition battery (6.3, §13): every mode starts and stops against
// real infrastructure (Postgres, MiniStack, Keycloak issuer) with no mocks.
// A second start of the same mode proves workers, servers and connections
// were released instead of leaking.

func fxModeConfig(t *testing.T, mode app.Mode, httpPort, adminPort int) app.Config {
	t.Helper()
	consumerKey := os.Getenv("SQS_CONSUMER_KEY")
	if consumerKey == "" {
		consumerKey = "consumeruser"
	}
	consumerSecret := os.Getenv("SQS_CONSUMER_SECRET")
	if consumerSecret == "" {
		consumerSecret = "consumersecret"
	}
	publisherKey := os.Getenv("SQS_PUBLISHER_KEY")
	if publisherKey == "" {
		publisherKey = "publisheruser"
	}
	publisherSecret := os.Getenv("SQS_PUBLISHER_SECRET")
	if publisherSecret == "" {
		publisherSecret = "publishersecret"
	}
	cfg := app.Config{
		Mode:           mode,
		HTTPAddr:       fmt.Sprintf("127.0.0.1:%d", httpPort),
		AdminAddr:      fmt.Sprintf("127.0.0.1:%d", adminPort),
		DatabaseURL:    testDSN(),
		SQSEndpoint:    sqsEndpoint(),
		OIDCIssuer:     "http://localhost:8081/realms/wallet",
		AWSRegion:      "us-east-1",
		SQSTxQueue:     "wager-transactions.fifo",
		SQSTxDLQ:       "wager-transactions-dlq.fifo",
		SQSEventsQueue: "wager-events.fifo",
	}
	// Role-scoped broker credentials, mirroring the compose wiring.
	switch mode {
	case app.ModeConsumer:
		cfg.AWSAccessKey, cfg.AWSSecretKey = consumerKey, consumerSecret
	case app.ModeWorkers:
		cfg.AWSAccessKey, cfg.AWSSecretKey = publisherKey, publisherSecret
	}
	return cfg
}

func getStatus(t *testing.T, url string) int {
	t.Helper()
	var code int
	deadline := time.Now().Add(10 * time.Second)
	for {
		resp, err := http.Get(url) //nolint:gosec,noctx
		if err == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			code = resp.StatusCode
			resp.Body.Close()
			return code
		}
		if time.Now().After(deadline) {
			t.Fatalf("GET %s: %v", url, err)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// waitPortClosed fails the test if url keeps serving: after Stop the
// listeners must be gone. Without this, a second boot could pass against a
// leaked server (OnStart never checks bind errors) instead of rebinding.
func waitPortClosed(t *testing.T, url string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		resp, err := http.Get(url) //nolint:gosec,noctx
		if err != nil {
			return
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if time.Now().After(deadline) {
			t.Fatalf("GET %s still serving after stop: leaked listener", url)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func startStopMode(t *testing.T, mode app.Mode, httpPort, adminPort int) {
	t.Helper()
	admin := fmt.Sprintf("http://127.0.0.1:%d", adminPort)
	public := fmt.Sprintf("http://127.0.0.1:%d", httpPort)
	start := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		fxApp := app.New(fxModeConfig(t, mode, httpPort, adminPort))
		if err := fxApp.Start(ctx); err != nil {
			t.Fatalf("mode %s start: %v", mode, err)
		}
		if code := getStatus(t, admin+"/metrics"); code != 200 {
			t.Fatalf("mode %s admin /metrics = %d, want 200", mode, code)
		}
		if mode == app.ModeAPI {
			if code := getStatus(t, public+"/health/live"); code != 200 {
				t.Fatalf("api /health/live = %d, want 200", code)
			}
		}
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer stopCancel()
		if err := fxApp.Stop(stopCtx); err != nil {
			t.Fatalf("mode %s stop: %v", mode, err)
		}
		waitPortClosed(t, admin+"/metrics")
		if mode == app.ModeAPI {
			waitPortClosed(t, public+"/health/live")
		}
	}
	start()
	// Rebind proves the first stop released servers, workers and pools.
	start()
}

func TestFxCompositionPerMode(t *testing.T) {
	startStopMode(t, app.ModeAPI, 18080, 18090)
	startStopMode(t, app.ModeConsumer, 18081, 18091)
	startStopMode(t, app.ModeWorkers, 18082, 18092)
}
