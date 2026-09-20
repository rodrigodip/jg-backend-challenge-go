package httpapi

import (
	"log/slog"

	"github.com/gin-gonic/gin"
)

// NewEngine builds the public HTTP handler: gin as the main handler with
// health/ready public and every business route behind authentication.
// Correlation runs globally so even 401s carry X-Correlation-Id; every
// request is logged as JSON and observed for latency.
func NewEngine(h *Handler, v Validator, log *slog.Logger) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(CorrelationMiddleware())
	r.Use(TracingMiddleware())
	r.Use(RequestLogger(log))
	r.Use(MetricsMiddleware())

	public := r.Group("/")
	protected := r.Group("/")
	protected.Use(AuthMiddleware(v))

	h.RegisterRoutes(public, protected)
	return r
}
