package httpapi

import (
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"
)

// RequestLogger emits one JSON log record per request through the process
// logger (already JSON in production): method, route template, status,
// latency and correlationId, plus providerId when authenticated. No
// credentials, tokens or financial payloads are logged.
func RequestLogger(log *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		attrs := []any{
			"method", c.Request.Method,
			"route", c.FullPath(),
			"status", c.Writer.Status(),
			"latencyMs", time.Since(start).Milliseconds(),
			"correlationId", CorrelationFrom(c),
		}
		if ident, ok := IdentityFrom(c); ok && ident.ProviderID != "" {
			attrs = append(attrs, "providerId", ident.ProviderID)
		}
		log.Info("http request", attrs...)
	}
}
