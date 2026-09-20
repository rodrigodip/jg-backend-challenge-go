package app

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/fx"
)

// NewLogger builds the process JSON logger.
func NewLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(os.Stdout, nil))
}

// adminMux serves operational endpoints on a separate port.
type adminMux struct{ *http.ServeMux }

// newAdminMux serves operational endpoints on a separate port.
func newAdminMux() adminMux {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	return adminMux{mux}
}

func writeJSON(w http.ResponseWriter, code int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_, _ = w.Write([]byte(body))
}

// readyHandler reports strict readiness of PostgreSQL and SQS.
func readyHandler(cfg Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		if err := checkPostgres(ctx, cfg.DatabaseURL); err != nil {
			writeJSON(w, http.StatusServiceUnavailable, `{"status":"not_ready","dependency":"postgres"}`)
			return
		}
		if err := checkSQS(ctx, cfg.SQSEndpoint); err != nil {
			writeJSON(w, http.StatusServiceUnavailable, `{"status":"not_ready","dependency":"sqs"}`)
			return
		}
		writeJSON(w, http.StatusOK, `{"status":"ready"}`)
	}
}

func checkPostgres(ctx context.Context, databaseURL string) error {
	u, err := url.Parse(databaseURL)
	if err != nil {
		return err
	}
	host := u.Hostname()
	if host == "" {
		host = "localhost"
	}
	port := u.Port()
	if port == "" {
		port = "5432"
	}
	d := &net.Dialer{Timeout: 2 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", net.JoinHostPort(host, port))
	if err != nil {
		return err
	}
	return conn.Close()
}

func checkSQS(ctx context.Context, endpoint string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"/_ministack/health", nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return &url.Error{Op: "GET", URL: endpoint, Err: http.ErrServerClosed}
	}
	return nil
}

// New builds the Fx application for the given config. The admin server
// (metrics) always runs; each mode appends its own module.
func New(cfg Config) *fx.App {
	opts := []fx.Option{
		fx.Supply(cfg),
		fx.Provide(NewLogger),
		fx.Provide(newAdminMux),
		fx.Invoke(registerAdminLifecycle),
	}
	switch cfg.Mode {
	case ModeAPI:
		opts = append(opts, ApiModule)
	case ModeConsumer:
		opts = append(opts, ConsumerModule)
	case ModeWorkers:
		opts = append(opts, WorkersModule)
	}
	return fx.New(opts...)
}

func registerAdminLifecycle(lc fx.Lifecycle, cfg Config, log *slog.Logger, admin adminMux) {
	adminSrv := &http.Server{Addr: cfg.AdminAddr, Handler: admin, ReadHeaderTimeout: 5 * time.Second}

	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			log.Info("starting admin server", "mode", string(cfg.Mode), "admin", cfg.AdminAddr)
			go func() {
				if err := adminSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
					log.Error("admin server stopped", "err", err)
				}
			}()
			return nil
		},
		OnStop: func(ctx context.Context) error {
			log.Info("stopping admin server", "mode", string(cfg.Mode))
			ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			if err := adminSrv.Shutdown(ctx); err != nil {
				log.Error("admin shutdown failed", "err", err)
			}
			return nil
		},
	})
}
