package httpapi

import (
	"github.com/gin-gonic/gin"
	"github.com/jg-backend-challenge/wallet/internal/obs"
	"go.opentelemetry.io/otel/attribute"
)

// TracingMiddleware opens one server span per request carrying the
// correlationId (D9). The route template is only known after routing, so the
// span is renamed post-handler with method, route and status attributes. The
// global provider is a no-op until the Fx composition installs stdout, which
// keeps tests without SetupStdout silent.
func TracingMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx, span := obs.Start(c.Request.Context(),
			"HTTP "+c.Request.Method, CorrelationFrom(c))
		defer span.End()
		c.Request = c.Request.WithContext(ctx)
		c.Next()
		route := c.FullPath()
		if route == "" {
			route = "unknown"
		}
		span.SetName(c.Request.Method + " " + route)
		span.SetAttributes(
			attribute.String("http.route", route),
			attribute.Int("http.status_code", c.Writer.Status()),
		)
		if ident, ok := IdentityFrom(c); ok && ident.ProviderID != "" {
			span.SetAttributes(attribute.String("providerId", ident.ProviderID))
		}
	}
}
