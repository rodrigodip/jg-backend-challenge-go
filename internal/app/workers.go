package app

import (
	"context"
	"log/slog"
	"os"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/jg-backend-challenge/wallet/internal/adapters/postgres"
	adapter "github.com/jg-backend-challenge/wallet/internal/adapters/sqs"
	"github.com/jg-backend-challenge/wallet/internal/wagering"
	"go.uber.org/fx"
)

// workerPollInterval is the D4 polling fallback for both worker loops.
const workerPollInterval = 500 * time.Millisecond

// WorkersModule wires the workers role (D1): the outbox publisher (with the
// publisher credentials) and the reference resumption loop.
var WorkersModule = fx.Module("workers",
	fx.Provide(NewStore),
	fx.Provide(NewWageringService),
	fx.Provide(NewSQSClient),
	fx.Provide(NewPublisher),
	fx.Invoke(runWorkers),
)

// NewPublisher resolves the events queue and builds the broker publisher.
// Like NewSQSClient, it bounds its own startup context (see consumer.go).
func NewPublisher(client *sqs.Client, cfg Config, log *slog.Logger) (*adapter.SQSPublisher, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	eventsURL, err := adapter.QueueURL(ctx, client, cfg.SQSEventsQueue)
	if err != nil {
		return nil, err
	}
	return &adapter.SQSPublisher{Client: client, EventsURL: eventsURL, Log: log}, nil
}

// workerOwner identifies claim leases per instance for cross-instance
// recovery attribution.
func workerOwner(role string) string {
	if h, err := os.Hostname(); err == nil && h != "" {
		return role + "-" + h
	}
	return role
}

func runWorkers(lc fx.Lifecycle, store *postgres.Store, svc *wagering.Service, pub *adapter.SQSPublisher, log *slog.Logger) {
	ctx, cancel := context.WithCancel(context.Background())
	lc.Append(fx.Hook{
		OnStart: func(context.Context) error {
			owner := workerOwner("workers")
			log.Info("starting workers", "owner", owner)
			go adapter.RunOutboxPublisher(ctx, store, pub, owner, workerPollInterval, log)
			go adapter.RunReferenceWorker(ctx, svc, owner, workerPollInterval, log)
			return nil
		},
		OnStop: func(context.Context) error {
			log.Info("stopping workers")
			cancel()
			return nil
		},
	})
}
