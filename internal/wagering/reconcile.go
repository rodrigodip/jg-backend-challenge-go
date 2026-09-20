package wagering

import (
	"context"
	"encoding/base64"
	"fmt"
	"log/slog"
	"strconv"

	"github.com/jg-backend-challenge/wallet/internal/domain"
	"github.com/jg-backend-challenge/wallet/internal/ports"
)

// Ledger cursor and reconciliation (3.4).

const (
	// LedgerDefaultLimit and LedgerMaxLimit document the paginated contract.
	LedgerDefaultLimit = 50
	LedgerMaxLimit     = 200
)

// LedgerPage is one cursor page: entries after the opaque cursor.
type LedgerPage struct {
	Entries    []*ports.LedgerRecord
	NextCursor string // empty when the page is complete
}

// EncodeCursor renders the opaque cursor from the last seen wallet_version.
func EncodeCursor(version int64) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatInt(version, 10)))
}

// DecodeCursor parses the opaque cursor; empty means "from the beginning".
func DecodeCursor(cursor string) (int64, error) {
	if cursor == "" {
		return 0, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return 0, correctable(CodeInvalidReq, "invalid ledger cursor")
	}
	v, err := strconv.ParseInt(string(raw), 10, 64)
	if err != nil || v < 0 {
		return 0, correctable(CodeInvalidReq, "invalid ledger cursor")
	}
	return v, nil
}

// PageLedger serves GET /wallets/:id/ledger semantics: stable order by the
// per-wallet sequence, no skips or duplicates under concurrent appends.
func (s *Service) PageLedger(walletID, cursor string, limit int) (*LedgerPage, error) {
	after, err := DecodeCursor(cursor)
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = LedgerDefaultLimit
	}
	if limit > LedgerMaxLimit {
		limit = LedgerMaxLimit
	}
	// Over-fetch by one to detect the next page without COUNT.
	rows, err := s.DB.Ledger().Page(walletID, after, limit+1)
	if err != nil {
		return nil, err
	}
	// Page/Ledger run outside a tx (read-only snapshot per statement).
	page := &LedgerPage{}
	if len(rows) > limit {
		rows = rows[:limit]
		page.NextCursor = EncodeCursor(rows[len(rows)-1].WalletVersion)
	}
	page.Entries = rows
	return page, nil
}

// Reconciliation is the non-destructive balance proof: stored (the wallet
// row) vs calculated (replayed ledger including the opening), with their
// difference (stored minus calculated) and the entry count. Zero-opening
// wallets reconcile with checkedEntries 0.
type Reconciliation struct {
	WalletID          string
	StoredBalance     domain.Money
	CalculatedBalance domain.Money
	Difference        domain.Money
	Consistent        bool
	CheckedEntries    int
}

// DivergenceReporter receives reconciliation divergences (metrics/logs
// adapters in block 4 wire the Prometheus counter + structured log).
type DivergenceReporter interface {
	ReportDivergence(ctx context.Context, walletID, stored, calculated, difference string, checked int)
}

// Reporter is the process-wide divergence sink (nil = log only).
var Reporter DivergenceReporter

// Reconcile rebuilds the balance from the ledger in REPEATABLE READ and
// compares it with the stored row. It never writes: divergences surface in
// the response, a structured log and the reporter metric.
func (s *Service) Reconcile(walletID string) (*Reconciliation, error) {
	var out *Reconciliation
	if err := s.DB.TransactRepeatableRead(func(db ports.DB) error {
		wallet, err := db.Wallet().Get(walletID)
		if err != nil {
			return err
		}
		if wallet == nil {
			return correctable(CodeWalletMissing, "wallet not found")
		}
		entries, err := db.Ledger().AllOrdered(walletID)
		if err != nil {
			return err
		}
		stored, err := domain.NewMoneyFromMinor(wallet.BalanceMinor, wallet.Currency)
		if err != nil {
			return err
		}
		calculated, err := domain.NewMoneyFromMinor(0, wallet.Currency)
		if err != nil {
			return err
		}
		for _, e := range entries {
			m, err := domain.NewMoneyFromMinor(e.AmountMinor, e.Currency)
			if err != nil {
				return err
			}
			switch e.Direction {
			case "CREDIT":
				calculated, err = calculated.Add(m)
			case "DEBIT":
				calculated, err = calculated.Sub(m)
			default:
				return fmt.Errorf("unknown ledger direction %q", e.Direction)
			}
			if err != nil {
				return err
			}
		}
		diff, err := stored.Sub(calculated)
		if err != nil {
			return err
		}
		cmp, err := stored.Cmp(calculated)
		if err != nil {
			return err
		}
		out = &Reconciliation{
			WalletID: walletID, StoredBalance: stored, CalculatedBalance: calculated,
			Difference: diff, Consistent: cmp == 0, CheckedEntries: len(entries),
		}
		return nil
	}); err != nil {
		return nil, err
	}
	if !out.Consistent {
		slog.Warn("ledger divergence",
			"walletId", out.WalletID,
			"stored", out.StoredBalance.String(),
			"calculated", out.CalculatedBalance.String(),
			"difference", out.Difference.String(),
			"checkedEntries", out.CheckedEntries)
		if Reporter != nil {
			Reporter.ReportDivergence(context.Background(), out.WalletID,
				out.StoredBalance.String(), out.CalculatedBalance.String(),
				out.Difference.String(), out.CheckedEntries)
		}
	}
	return out, nil
}
