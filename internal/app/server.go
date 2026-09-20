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

// publicMux serves business routes (wired in later blocks) and health checks.
type publicMux struct{ *http.ServeMux }

// adminMux serves operational endpoints on a separate port.
type adminMux struct{ *http.ServeMux }

// newPublicMux serves business routes (wired in later blocks) and health checks.
func newPublicMux(cfg Config) publicMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/health/live", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"alive"}`))
	})
	mux.HandleFunc("/health/ready", readyHandler(cfg))
	return publicMux{mux}
}

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

// New builds the Fx application for the given config.
func New(cfg Config) *fx.App {
	return fx.New(
		fx.Supply(cfg),
		fx.Provide(NewLogger),
		fx.Provide(newPublicMux, newAdminMux),
		fx.Invoke(registerLifecycle),
	)
}

func registerLifecycle(lc fx.Lifecycle, cfg Config, log *slog.Logger, public publicMux, admin adminMux) {
	publicSrv := &http.Server{Addr: cfg.HTTPAddr, Handler: public, ReadHeaderTimeout: 5 * time.Second}
	adminSrv := &http.Server{Addr: cfg.AdminAddr, Handler: admin, ReadHeaderTimeout: 5 * time.Second}

	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			log.Info("starting servers", "mode", string(cfg.Mode), "http", cfg.HTTPAddr, "admin", cfg.AdminAddr)
			go func() {
				if err := publicSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
					log.Error("public server stopped", "err", err)
				}
			}()
			go func() {
				if err := adminSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
					log.Error("admin server stopped", "err", err)
				}
			}()
			return nil
		},
		OnStop: func(ctx context.Context) error {
			log.Info("stopping servers", "mode", string(cfg.Mode))
			ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			if err := publicSrv.Shutdown(ctx); err != nil {
				log.Error("public shutdown failed", "err", err)
			}
			if err := adminSrv.Shutdown(ctx); err != nil {
				log.Error("admin shutdown failed", "err", err)
			}
			return nil
		},
	})

	if cfg.Mode != ModeAPI {
		ctx, cancel := context.WithCancel(context.Background())
		lc.Append(fx.Hook{
			OnStart: func(context.Context) error {
				go workerHeartbeat(ctx, log, cfg.Mode)
				return nil
			},
			OnStop: func(context.Context) error {
				cancel()
				return nil
			},
		})
	}
}

// workerHeartbeat keeps consumer/workers processes observable until real
// workers land in later blocks. It stops on context cancellation.
func workerHeartbeat(ctx context.Context, log *slog.Logger, mode Mode) {
	t := time.NewTicker(10 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			log.Info("worker heartbeat", "mode", string(mode))
		}
	}
}
