// Package postgres holds GORM-based persistence with explicit transactions,
// locks and constraints. GORM is only an executor: no AutoMigrate,
// no gorm.Model, no Save on immutable rows.
package postgres

import (
	"time"

	"gorm.io/gorm"
)

// WalletModel maps wallets. Balance lives in minor units, version starts
// at 1 and increments only on balance changes.
type WalletModel struct {
	ID           string    `gorm:"column:id;primaryKey;type:uuid"`
	PlayerID     string    `gorm:"column:player_id;type:uuid;index"`
	Currency     string    `gorm:"column:currency;type:char(3)"`
	BalanceMinor int64     `gorm:"column:balance_minor"`
	Version      int64     `gorm:"column:version"`
	CreatedAt    time.Time `gorm:"column:created_at"`
	UpdatedAt    time.Time `gorm:"column:updated_at"`
}

// TableName pins the table explicitly.
func (WalletModel) TableName() string { return "wallets" }

// WagerTxModel maps wager_transactions. Nullable columns use pointers;
// immutable after insert (repositories never update except for state
// transitions and reversal bookkeeping on the same row).
type WagerTxModel struct {
	ID                    string    `gorm:"column:id;primaryKey;type:uuid"`
	Origin                string    `gorm:"column:origin"`
	ProviderID            *string   `gorm:"column:provider_id"`
	ExternalID            *string   `gorm:"column:external_transaction_id"`
	IdempotencyKey        *string   `gorm:"column:idempotency_key"`
	PayloadHash           *string   `gorm:"column:payload_hash"`
	WalletID              string    `gorm:"column:wallet_id;type:uuid"`
	PlayerID              string    `gorm:"column:player_id;type:uuid"`
	RoundID               *string   `gorm:"column:round_id"`
	GameID                *string   `gorm:"column:game_id"`
	Kind                  string    `gorm:"column:kind"`
	AmountMinor           int64     `gorm:"column:amount_minor"`
	Currency              string    `gorm:"column:currency;type:char(3)"`
	ReferenceExternalID   *string   `gorm:"column:reference_external_transaction_id"`
	ReferenceTxID         *string   `gorm:"column:reference_transaction_id;type:uuid"`
	State                 string    `gorm:"column:state"`
	FailureCode           *string   `gorm:"column:failure_code"`
	ResultBalanceMinor    *int64    `gorm:"column:result_balance_minor"`
	ResultCurrency        *string   `gorm:"column:result_currency;type:char(3)"`
	ReversedByKind        *string   `gorm:"column:reversed_by_kind"`
	ReversedByTxID        *string   `gorm:"column:reversed_by_transaction_id;type:uuid"`
	WalletVersionObserved *int64    `gorm:"column:wallet_version_observed"`
	CreatedAt             time.Time `gorm:"column:created_at"`
	UpdatedAt             time.Time `gorm:"column:updated_at"`
}

// TableName pins the table explicitly.
func (WagerTxModel) TableName() string { return "wager_transactions" }

// LedgerModel maps wallet_ledger_entries. Append-only: repositories only
// Create, never update or delete (the DB trigger enforces it too).
type LedgerModel struct {
	ID            string    `gorm:"column:id;primaryKey;type:uuid"`
	WalletID      string    `gorm:"column:wallet_id;type:uuid"`
	TransactionID string    `gorm:"column:transaction_id;type:uuid"`
	Direction     string    `gorm:"column:direction"`
	AmountMinor   int64     `gorm:"column:amount_minor"`
	Currency      string    `gorm:"column:currency;type:char(3)"`
	BeforeMinor   int64     `gorm:"column:balance_before_minor"`
	AfterMinor    int64     `gorm:"column:balance_after_minor"`
	WalletVersion int64     `gorm:"column:wallet_version"`
	CreatedAt     time.Time `gorm:"column:created_at"`
}

// TableName pins the table explicitly.
func (LedgerModel) TableName() string { return "wallet_ledger_entries" }

// InboxModel maps inbox. Natural primary key (consumer, message).
type InboxModel struct {
	Consumer    string     `gorm:"column:consumer_name;primaryKey"`
	MessageID   string     `gorm:"column:message_id;primaryKey"`
	PayloadHash string     `gorm:"column:payload_hash"`
	ReceivedAt  time.Time  `gorm:"column:received_at"`
	ConcludedAt *time.Time `gorm:"column:concluded_at"`
}

// TableName pins the table explicitly.
func (InboxModel) TableName() string { return "inbox" }

// OutboxModel maps outbox. Published via worker claim with lease.
type OutboxModel struct {
	EventID        string     `gorm:"column:event_id;primaryKey;type:uuid"`
	AggregateID    string     `gorm:"column:aggregate_id;type:uuid"`
	EventType      string     `gorm:"column:event_type"`
	Payload        []byte     `gorm:"column:payload;type:jsonb"`
	OccurredAt     time.Time  `gorm:"column:occurred_at"`
	Attempts       int        `gorm:"column:attempts"`
	NextSendAt     time.Time  `gorm:"column:next_send_at"`
	PublishedAt    *time.Time `gorm:"column:published_at"`
	LeaseOwner     *string    `gorm:"column:lease_owner"`
	LeaseExpiresAt *time.Time `gorm:"column:lease_expires_at"`
}

// TableName pins the table explicitly.
func (OutboxModel) TableName() string { return "outbox" }

// WorkModel maps work_items. Lease columns drive cross-instance claims.
type WorkModel struct {
	TransactionID  string     `gorm:"column:transaction_id;primaryKey;type:uuid"`
	Kind           string     `gorm:"column:kind"`
	RefAttempts    int        `gorm:"column:ref_attempts"`
	InfraAttempts  int        `gorm:"column:infra_attempts"`
	NextAttemptAt  time.Time  `gorm:"column:next_attempt_at"`
	LeaseOwner     *string    `gorm:"column:lease_owner"`
	LeaseExpiresAt *time.Time `gorm:"column:lease_expires_at"`
	CreatedAt      time.Time  `gorm:"column:created_at"`
}

// TableName pins the table explicitly.
func (WorkModel) TableName() string { return "work_items" }

// ensure gorm import is used when models file stands alone.
var _ = gorm.ErrRecordNotFound
