package sqs

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Broker-side metrics on the default registry (served by admin /metrics).
// Retry/DLQ/outbox-delay families promised in 4.3 land here with block 5.
var (
	consumeResults = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "wallet_sqs_consume_total",
		Help: "SQS messages consumed by durable outcome.",
	}, []string{"result"})

	dlqMoves = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "wallet_sqs_dlq_total",
		Help: "Messages routed to the DLQ by reason.",
	}, []string{"reason"})

	visibilityRetries = promauto.NewCounter(prometheus.CounterOpts{
		Name: "wallet_sqs_visibility_retries_total",
		Help: "Transient treatments deferred via ChangeMessageVisibility.",
	})

	outboxPublished = promauto.NewCounter(prometheus.CounterOpts{
		Name: "wallet_outbox_published_total",
		Help: "Outbox events published to wager-events.fifo.",
	})

	outboxPublishErrors = promauto.NewCounter(prometheus.CounterOpts{
		Name: "wallet_outbox_publish_errors_total",
		Help: "Outbox publish attempts that failed and backed off.",
	})

	outboxPublishDelay = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "wallet_outbox_publish_delay_seconds",
		Help:    "Commit-to-publish delay of outbox events.",
		Buckets: prometheus.DefBuckets,
	})
)
