package app

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jg-backend-challenge/wallet/internal/adapters/http"
	"github.com/jg-backend-challenge/wallet/internal/adapters/postgres"
	"github.com/jg-backend-challenge/wallet/internal/ports"
	"github.com/jg-backend-challenge/wallet/internal/wagering"
	"go.uber.org/fx"
)

// ApiModule wires the api role (D1): postgres store, wagering service,
// validator, handler and gin engine, plus the public server lifecycle.
//
// The validator binding is a closed StaticValidator until task 4.2 binds
// the OIDC/Keycloak validator: business endpoints answer 401, health stays
// public.
var ApiModule = fx.Module("api",
	fx.Provide(NewStore),
	fx.Provide(NewWageringService),
	fx.Provide(NewValidator),
	fx.Provide(httpapi.NewHandler),
	fx.Provide(NewPublicEngine),
	fx.Invoke(registerPublicLifecycle),
)

// NewStore opens the application database handle. Migrations stay owned by
// the goose migrate job; Open never migrates.
func NewStore(cfg Config) (*postgres.Store, error) {
	return postgres.Open(cfg.DatabaseURL)
}

// NewWageringService builds the transactional wager use-case.
func NewWageringService(store *postgres.Store) *wagering.Service {
	return &wagering.Service{DB: store, Clock: ports.SystemClock{}}
}

// NewValidator provides the bearer validator. Placeholder for task 4.2:
// deny-closed until the OIDC issuer is wired.
func NewValidator() httpapi.Validator {
	return httpapi.StaticValidator{Tokens: map[string]httpapi.Identity{}}
}

// NewPublicEngine builds the gin engine with strict readiness from Config.
func NewPublicEngine(h *httpapi.Handler, v httpapi.Validator, cfg Config) *gin.Engine {
	h.ReadyCheck = func(ctx context.Context) error {
		ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		if err := checkPostgres(ctx, cfg.DatabaseURL); err != nil {
			return err
		}
		return checkSQS(ctx, cfg.SQSEndpoint)
	}
	return httpapi.NewEngine(h, v)
}

func registerPublicLifecycle(lc fx.Lifecycle, cfg Config, log *slog.Logger, public *gin.Engine) {
	publicSrv := &http.Server{Addr: cfg.HTTPAddr, Handler: public, ReadHeaderTimeout: 5 * time.Second}

	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			log.Info("starting public server", "mode", string(cfg.Mode), "http", cfg.HTTPAddr)
			go func() {
				if err := publicSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
					log.Error("public server stopped", "err", err)
				}
			}()
			return nil
		},
		OnStop: func(ctx context.Context) error {
			log.Info("stopping public server", "mode", string(cfg.Mode))
			ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			if err := publicSrv.Shutdown(ctx); err != nil {
				log.Error("public shutdown failed", "err", err)
			}
			return nil
		},
	})
}
