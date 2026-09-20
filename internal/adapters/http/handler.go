package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/jg-backend-challenge/wallet/internal/domain"
	"github.com/jg-backend-challenge/wallet/internal/wagering"
)

// Handler serves the business routes over a wagering.Service. AuthZ follows
// the ADR-0010 matrix: provider operates only its own scope, internal owns
// wallets/ledger/reconciliation and reads any transaction.
type Handler struct {
	Svc *wagering.Service
	// ReadyCheck reports strict readiness of PostgreSQL and SQS. It is
	// provided by the app package from Config so handlers stay
	// infrastructure-free; nil means always ready (unit tests).
	ReadyCheck ReadyFunc
}

// NewHandler builds the business handler.
func NewHandler(svc *wagering.Service) *Handler { return &Handler{Svc: svc} }

// RegisterRoutes wires business, query and health routes. AuthMiddleware must
// already guard the protected group; health stays public.
func (h *Handler) RegisterRoutes(public, protected *gin.RouterGroup) {
	public.GET("/health/live", h.live)
	public.GET("/health/ready", h.ready)

	protected.POST("/wallets", h.openWallet)
	protected.GET("/wallets/:id", h.getWallet)
	protected.GET("/wallets/:id/ledger", h.pageLedger)
	protected.GET("/wallets/:id/reconciliation", h.reconcile)

	protected.POST("/wagering/transactions", h.submitTransaction)
	protected.GET("/wagering/transactions/:id", h.getTransaction)
}

// ReadyFunc reports strict readiness of dependencies.
type ReadyFunc func(ctx context.Context) error

func (h *Handler) live(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "alive"})
}

func (h *Handler) ready(c *gin.Context) {
	if h.ReadyCheck == nil {
		c.JSON(http.StatusOK, gin.H{"status": "ready"})
		return
	}
	if err := h.ReadyCheck(c.Request.Context()); err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"status": "not_ready"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ready"})
}

// openWalletRequest is POST /wallets.
type openWalletRequest struct {
	PlayerID      string `json:"playerId"`
	Currency      string `json:"currency"`
	InitialAmount string `json:"initialAmount"`
}

func (h *Handler) openWallet(c *gin.Context) {
	if _, ok := requireRole(c, RoleInternal); !ok {
		return
	}
	var req openWalletRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		abortError(c, http.StatusBadRequest, "INVALID_REQUEST", "invalid body")
		return
	}
	if strings.TrimSpace(req.PlayerID) == "" || strings.TrimSpace(req.Currency) == "" || strings.TrimSpace(req.InitialAmount) == "" {
		abortError(c, http.StatusBadRequest, "INVALID_REQUEST", "playerId, currency and initialAmount are required")
		return
	}
	rec, err := h.Svc.OpenWallet(req.PlayerID, req.Currency, req.InitialAmount)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusCreated, walletBody(rec.ID, rec.PlayerID, rec.Currency, rec.BalanceMinor, rec.Version))
}

func (h *Handler) getWallet(c *gin.Context) {
	if _, ok := requireRole(c, RoleInternal); !ok {
		return
	}
	rec, err := h.Svc.DB.Wallet().Get(c.Param("id"))
	if err != nil {
		writeInfraError(c, err)
		return
	}
	if rec == nil {
		abortError(c, http.StatusNotFound, "WALLET_NOT_FOUND", "wallet not found")
		return
	}
	c.JSON(http.StatusOK, walletBody(rec.ID, rec.PlayerID, rec.Currency, rec.BalanceMinor, rec.Version))
}

func (h *Handler) pageLedger(c *gin.Context) {
	if _, ok := requireRole(c, RoleInternal); !ok {
		return
	}
	walletID := c.Param("id")
	wallet, err := h.Svc.DB.Wallet().Get(walletID)
	if err != nil {
		writeInfraError(c, err)
		return
	}
	if wallet == nil {
		abortError(c, http.StatusNotFound, "WALLET_NOT_FOUND", "wallet not found")
		return
	}
	limit := 0
	if raw := c.Query("limit"); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil {
			abortError(c, http.StatusBadRequest, "INVALID_REQUEST", "invalid limit")
			return
		}
		limit = v
	}
	page, err := h.Svc.PageLedger(walletID, c.Query("cursor"), limit)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	entries := make([]gin.H, 0, len(page.Entries))
	for _, e := range page.Entries {
		entries = append(entries, gin.H{
			"transactionId": e.TransactionID,
			"direction":     e.Direction,
			"amount":        minorBody(e.AmountMinor, e.Currency),
			"balanceBefore": minorBody(e.BeforeMinor, e.Currency),
			"balanceAfter":  minorBody(e.AfterMinor, e.Currency),
			"walletVersion": e.WalletVersion,
		})
	}
	c.JSON(http.StatusOK, gin.H{"entries": entries, "nextCursor": page.NextCursor})
}

func (h *Handler) reconcile(c *gin.Context) {
	if _, ok := requireRole(c, RoleInternal); !ok {
		return
	}
	rec, err := h.Svc.Reconcile(c.Param("id"))
	if err != nil {
		var ce *wagering.CorrectableError
		if errors.As(err, &ce) && ce.Code == wagering.CodeWalletMissing {
			abortError(c, http.StatusNotFound, "WALLET_NOT_FOUND", "wallet not found")
			return
		}
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"walletId":          rec.WalletID,
		"storedBalance":     moneyBody(rec.StoredBalance),
		"calculatedBalance": moneyBody(rec.CalculatedBalance),
		"difference":        moneyBody(rec.Difference),
		"consistent":        rec.Consistent,
		"checkedEntries":    rec.CheckedEntries,
	})
}

// submitRequest is POST /wagering/transactions.
type submitRequest struct {
	ProviderID          string `json:"providerId"`
	ExternalID          string `json:"externalTransactionId"`
	PlayerID            string `json:"playerId"`
	WalletID            string `json:"walletId"`
	RoundID             string `json:"roundId"`
	GameID              string `json:"gameId"`
	Kind                string `json:"kind"`
	Amount              string `json:"amount"`
	Currency            string `json:"currency"`
	ReferenceExternalID string `json:"referenceExternalTransactionId"`
}

func (h *Handler) submitTransaction(c *gin.Context) {
	ident, ok := requireRole(c, RoleProvider)
	if !ok {
		return
	}
	key := strings.TrimSpace(c.GetHeader(IdempotencyKeyHead))
	if key == "" || len(key) > 255 {
		abortError(c, http.StatusBadRequest, "INVALID_REQUEST", "Idempotency-Key header is required (1-255 chars)")
		return
	}
	var req submitRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		abortError(c, http.StatusBadRequest, "INVALID_REQUEST", "invalid body")
		return
	}
	if req.ProviderID != ident.ProviderID {
		// Scope divergence: 403 with no data and no financial effect.
		abortError(c, http.StatusForbidden, "PROVIDER_FORBIDDEN", "forbidden")
		return
	}
	result, err := h.Svc.Submit(wagering.SubmitInput{
		ProviderID: req.ProviderID, ExternalID: req.ExternalID,
		IdempotencyKey: key, PlayerID: req.PlayerID, WalletID: req.WalletID,
		RoundID: req.RoundID, GameID: req.GameID,
		Kind:              domain.TransactionKind(req.Kind),
		AmountText:        req.Amount,
		Currency:          req.Currency,
		ReferenceExternal: req.ReferenceExternalID,
		CorrelationID:     CorrelationFrom(c),
	})
	if err != nil {
		writeServiceError(c, err)
		return
	}
	writeSubmitResult(c, result)
}

func (h *Handler) getTransaction(c *gin.Context) {
	ident, ok := IdentityFrom(c)
	if !ok {
		abortError(c, http.StatusForbidden, "PROVIDER_FORBIDDEN", "forbidden")
		return
	}
	rec, err := h.Svc.DB.Tx().Get(c.Param("id"))
	if err != nil {
		writeInfraError(c, err)
		return
	}
	if rec == nil {
		abortError(c, http.StatusNotFound, "TRANSACTION_NOT_FOUND", "transaction not found")
		return
	}
	// Providers see only their own scope; internal reads anything. 403
	// carries no data.
	if !ident.HasRole(RoleInternal) {
		if !ident.HasRole(RoleProvider) || rec.ProviderID == nil || *rec.ProviderID != ident.ProviderID {
			abortError(c, http.StatusForbidden, "PROVIDER_FORBIDDEN", "forbidden")
			return
		}
	}
	body := gin.H{
		"transactionId":    rec.ID,
		"kind":             rec.Kind,
		"status":           rec.State,
		"idempotentReplay": false,
		"playerId":         rec.PlayerID,
		"walletId":         rec.WalletID,
		"amount":           minorBody(rec.AmountMinor, rec.Currency),
	}
	if rec.ProviderID != nil {
		body["providerId"] = *rec.ProviderID
	}
	if rec.ExternalID != nil {
		body["externalTransactionId"] = *rec.ExternalID
	}
	if rec.FailureCode != nil {
		body["failureCode"] = *rec.FailureCode
	}
	if rec.ReferenceExternalID != nil {
		body["referenceExternalTransactionId"] = *rec.ReferenceExternalID
	}
	if rec.ResultBalanceMinor != nil && rec.ResultCurrency != nil {
		body["balance"] = minorBody(*rec.ResultBalanceMinor, *rec.ResultCurrency)
	}
	if rec.WalletVersionObserved != nil {
		body["walletVersion"] = *rec.WalletVersionObserved
	}
	c.JSON(http.StatusOK, body)
}

// writeSubmitResult maps the durable outcome to the HTTP contract: 201 first
// processing, 200 replay of a processed transaction, 422 business rejection
// (first or replayed, always with the persisted body), 202 pending without
// balance.
func writeSubmitResult(c *gin.Context, r *wagering.SubmitResult) {
	status := "PROCESSED"
	code := http.StatusCreated
	switch r.Outcome {
	case wagering.OutcomeRejected:
		status, code = "REJECTED", http.StatusUnprocessableEntity
	case wagering.OutcomePending:
		status, code = "PENDING", http.StatusAccepted
	case wagering.OutcomeReplay:
		if r.FailureCode != "" {
			status, code = "REJECTED", http.StatusUnprocessableEntity
		} else {
			code = http.StatusOK
		}
	}
	body := gin.H{
		"transactionId":    r.TransactionID,
		"status":           status,
		"idempotentReplay": r.IdempotentReplay,
	}
	if r.FailureCode != "" {
		body["failureCode"] = r.FailureCode
	}
	if r.Balance != nil {
		body["balance"] = moneyBody(*r.Balance)
		body["walletVersion"] = r.WalletVersion
	}
	c.JSON(code, body)
}

// writeServiceError maps service errors: correctables to 400 (persist
// nothing, reserve no key), conflicts to 409, everything else to 503 with
// Retry-After as a transient signal.
func writeServiceError(c *gin.Context, err error) {
	var correctable *wagering.CorrectableError
	if errors.As(err, &correctable) {
		abortError(c, http.StatusBadRequest, correctable.Code, correctable.Message)
		return
	}
	var conflict *wagering.ConflictError
	if errors.As(err, &conflict) {
		abortError(c, http.StatusConflict, conflict.Code, conflict.Message)
		return
	}
	writeInfraError(c, err)
}

func writeInfraError(c *gin.Context, err error) {
	c.Header("Retry-After", "1")
	abortError(c, http.StatusServiceUnavailable, "TEMPORARILY_UNAVAILABLE", "temporarily unavailable")
	_ = err
}

func walletBody(id, playerID, currency string, balanceMinor, version int64) gin.H {
	return gin.H{
		"walletId": id,
		"playerId": playerID,
		"currency": currency,
		"balance":  minorBody(balanceMinor, currency),
		"version":  version,
	}
}

func minorBody(minor int64, currency string) gin.H {
	if m, err := domain.NewMoneyFromMinor(minor, currency); err == nil {
		return moneyBody(m)
	}
	return gin.H{"amount": "0", "currency": currency}
}

func moneyBody(m domain.Money) gin.H {
	return gin.H{"amount": m.String(), "currency": m.Currency()}
}
