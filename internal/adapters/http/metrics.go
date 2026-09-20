package httpapi

import (
	"context"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jg-backend-challenge/wallet/internal/wagering"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Metrics emitted by the HTTP layer, registered on the default registry so
// the existing admin /metrics endpoint (promhttp) exposes them without
// further wiring.
//
// Auth covers what the api role can observe: outcomes by status,
// idempotent replays (duplicates), idempotency conflicts, request latency
// and reconciliation divergences. Retries, DLQ moves and outbox age belong
// to the consumer/workers roles and land with auth.
var (
	txResults = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "wallet_tx_results_total",
		Help: "Wager transaction outcomes served over HTTP.",
	}, []string{"outcome"})

	idempotentReplays = promauto.NewCounter(prometheus.CounterOpts{
		Name: "wallet_idempotent_replays_total",
		Help: "Replayed submissions served without reapplying money.",
	})

	idempotencyConflicts = promauto.NewCounter(prometheus.CounterOpts{
		Name: "wallet_idempotency_conflicts_total",
		Help: "Submissions rejected with idempotency or scope conflicts.",
	})

	httpLatency = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "wallet_http_request_duration_seconds",
		Help:    "Public HTTP request latency.",
		Buckets: prometheus.DefBuckets,
	}, []string{"method", "route", "status"})

	ledgerDivergences = promauto.NewCounter(prometheus.CounterOpts{
		Name: "ledger_divergences_total",
		Help: "Reconciliations where stored balance differs from replayed ledger.",
	})
)

// recordTxResult counts one durable submit outcome. outcome is one of
// processed, rejected, pending or replay.
func recordTxResult(r *wagering.SubmitResult) {
	outcome := "processed"
	switch r.Outcome {
	case wagering.OutcomeRejected:
		outcome = "rejected"
	case wagering.OutcomePending:
		outcome = "pending"
	case wagering.OutcomeReplay:
		outcome = "replay"
	}
	txResults.WithLabelValues(outcome).Inc()
	if r.IdempotentReplay {
		idempotentReplays.Inc()
	}
}

// MetricsMiddleware observes per-request latency by route. Route labels use
// the gin template (e.g. /wallets/:id), so cardinality stays bounded.
func MetricsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		route := c.FullPath()
		if route == "" {
			route = "unknown"
		}
		httpLatency.WithLabelValues(c.Request.Method, route,
			statusClass(c.Writer.Status())).Observe(time.Since(start).Seconds())
	}
}

func statusClass(code int) string {
	switch {
	case code < 300:
		return "2xx"
	case code < 400:
		return "3xx"
	case code < 500:
		return "4xx"
	default:
		return "5xx"
	}
}

// DivergenceMetrics implements wagering.DivergenceReporter by counting every
// reported divergence. Wire it as wagering.Reporter in the api composition.
type DivergenceMetrics struct{}

// ReportDivergence counts the divergence; the wallet id and balances stay in
// the structured log emitted by wagering.Reconcile, never in labels.
func (DivergenceMetrics) ReportDivergence(_ context.Context, _, _, _, _ string, _ int) {
	ledgerDivergences.Inc()
}
