package app

import (
	"context"
	"log/slog"

	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/jg-backend-challenge/wallet/internal/adapters/postgres"
	adapter "github.com/jg-backend-challenge/wallet/internal/adapters/sqs"
	"github.com/jg-backend-challenge/wallet/internal/ports"
	"github.com/jg-backend-challenge/wallet/internal/wagering"
	"go.uber.org/fx"
)

// ConsumerModule wires the consumer role (D1): postgres store, wagering
// service, role-scoped SQS client, queue resolution and the poll loop with
// SIGTERM drain.
var ConsumerModule = fx.Module("consumer",
	fx.Provide(NewStore),
	fx.Provide(NewWageringService),
	fx.Provide(NewSQSClient),
	fx.Provide(NewConsumer),
	fx.Invoke(runConsumer),
)

// NewSQSClient builds the SQS client with this role's credentials. It is
// shared by the consumer and workers modules; each role's process carries
// its own keypair via the standard AWS env (5.3).
func NewSQSClient(ctx context.Context, cfg Config) (*sqs.Client, error) {
	return adapter.NewClient(ctx, cfg.SQSEndpoint, cfg.AWSRegion, cfg.AWSAccessKey, cfg.AWSSecretKey)
}

// NewConsumer resolves the queues and builds the poller.
func NewConsumer(ctx context.Context, client *sqs.Client, cfg Config, store *postgres.Store, log *slog.Logger) (*adapter.Consumer, error) {
	queueURL, err := adapter.QueueURL(ctx, client, cfg.SQSTxQueue)
	if err != nil {
		return nil, err
	}
	dlqURL, err := adapter.QueueURL(ctx, client, cfg.SQSTxDLQ)
	if err != nil {
		return nil, err
	}
	return &adapter.Consumer{
		Client: client, QueueURL: queueURL, DLQURL: dlqURL,
		Service: &wagering.Service{DB: store, Clock: ports.SystemClock{}},
		Log:     log,
	}, nil
}

func runConsumer(lc fx.Lifecycle, consumer *adapter.Consumer, log *slog.Logger) {
	ctx, cancel := context.WithCancel(context.Background())
	lc.Append(fx.Hook{
		OnStart: func(context.Context) error {
			log.Info("starting sqs consumer", "queue", consumer.QueueURL)
			go func() {
				if err := consumer.Run(ctx); err != nil {
					log.Error("sqs consumer stopped", "err", err)
				}
			}()
			return nil
		},
		OnStop: func(context.Context) error {
			log.Info("stopping sqs consumer")
			cancel()
			return nil
		},
	})
}
