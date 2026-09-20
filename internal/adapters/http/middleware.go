package httpapi

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/jg-backend-challenge/wallet/internal/wagering"
)

// Context keys for request-scoped values.
const (
	ctxIdentityKey     = "identity"
	ctxCorrelationKey  = "correlationID"
	CorrelationHeader  = "X-Correlation-Id"
	IdempotencyKeyHead = "Idempotency-Key"
)

// IdentityFrom returns the verified caller identity of the request.
func IdentityFrom(c *gin.Context) (Identity, bool) {
	id, ok := c.Get(ctxIdentityKey)
	if !ok {
		return Identity{}, false
	}
	ident, ok := id.(Identity)
	return ident, ok
}

// CorrelationFrom returns the request correlation id (always set by
// CorrelationMiddleware).
func CorrelationFrom(c *gin.Context) string {
	if v, ok := c.Get(ctxCorrelationKey); ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// CorrelationMiddleware propagates or generates the correlation id: inbound
// X-Correlation-Id wins, otherwise a UUID is minted. The value travels on
// the response header, the gin context and (via handlers) the wagering
// SubmitInput into outbox envelopes, logs and spans.
func CorrelationMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		corr := c.GetHeader(CorrelationHeader)
		if strings.TrimSpace(corr) == "" {
			corr = wagering.NewUUID()
		}
		c.Set(ctxCorrelationKey, corr)
		c.Header(CorrelationHeader, corr)
		c.Next()
	}
}

// AuthMiddleware enforces bearer authentication via the Validator. Missing
// or rejected credentials answer 401 with no financial effect and no data.
// Register it only on protected route groups; health stays public.
func AuthMiddleware(v Validator) gin.HandlerFunc {
	return func(c *gin.Context) {
		token := bearerToken(c.GetHeader("Authorization"))
		if token == "" {
			abortError(c, http.StatusUnauthorized, "UNAUTHENTICATED", "missing credentials")
			return
		}
		ident, err := v.ValidateToken(c.Request.Context(), token)
		if err != nil {
			abortError(c, http.StatusUnauthorized, "UNAUTHENTICATED", "invalid or expired credentials")
			return
		}
		c.Set(ctxIdentityKey, ident)
		c.Next()
	}
}

func bearerToken(header string) string {
	if header == "" {
		return ""
	}
	parts := strings.SplitN(header, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "bearer") {
		return ""
	}
	return strings.TrimSpace(parts[1])
}

// requireRole keeps handlers scoped: provider sends operations on its own
// scope, internal owns wallets/ledger/reconciliation plus any read. Role
// failures answer 403 PROVIDER_FORBIDDEN without data.
func requireRole(c *gin.Context, role string) (Identity, bool) {
	ident, ok := IdentityFrom(c)
	if !ok || !ident.HasRole(role) {
		abortError(c, http.StatusForbidden, "PROVIDER_FORBIDDEN", "forbidden")
		return Identity{}, false
	}
	return ident, true
}

func abortError(c *gin.Context, code int, errCode, msg string) {
	c.AbortWithStatusJSON(code, gin.H{"code": errCode, "message": msg})
}
