// Package ports declares inbound and outbound interfaces driven by the
// domain (repositories, clocks, publishers). Adapters implement them.
package ports

import "time"

// Clock abstracts time for leases, backoff and TTLs.
type Clock interface {
	Now() time.Time
}

// SystemClock is the production Clock.
type SystemClock struct{}

// Now returns the current UTC time.
func (SystemClock) Now() time.Time { return time.Now().UTC() }

// WalletRecord is the persistence view of a wallet row.
type WalletRecord struct {
	ID           string
	PlayerID     string
	Currency     string
	BalanceMinor int64
	Version      int64
}

// TxRecord is the persistence view of a wager_transactions row.
type TxRecord struct {
	ID                    string
	Origin                string // INTERNAL | EXTERNAL
	ProviderID            *string
	ExternalID            *string
	IdempotencyKey        *string
	PayloadHash           *string
	WalletID              string
	PlayerID              string
	RoundID               *string
	GameID                *string
	Kind                  string
	AmountMinor           int64
	Currency              string
	ReferenceExternalID   *string
	ReferenceTxID         *string
	State                 string
	FailureCode           *string
	ResultBalanceMinor    *int64
	ResultCurrency        *string
	ReversedByKind        *string
	ReversedByTxID        *string
	WalletVersionObserved *int64
}

// LedgerRecord is the persistence view of a wallet_ledger_entries row.
type LedgerRecord struct {
	ID            string
	WalletID      string
	TransactionID string
	Direction     string // DEBIT | CREDIT
	AmountMinor   int64
	Currency      string
	BeforeMinor   int64
	AfterMinor    int64
	WalletVersion int64
}

// OutboxRecord is the persistence view of an outbox row.
type OutboxRecord struct {
	EventID     string
	AggregateID string
	EventType   string
	Payload     []byte
	Attempts    int
}

// WorkItem is the persistence view of a work_items row.
type WorkItem struct {
	TransactionID  string
	Kind           string // initial | reference
	RefAttempts    int
	InfraAttempts  int
	NextAttemptAt  time.Time
	LeaseOwner     string
	LeaseExpiresAt *time.Time
	CreatedAt      time.Time
}

// WalletRepo reads and writes wallets. All methods run inside the caller's
// transaction; LockForUpdate serializes concurrent operations per wallet.
type WalletRepo interface {
	LockForUpdate(walletID string) (*WalletRecord, error)
	Insert(w *WalletRecord) error
	UpdateBalance(walletID string, balanceMinor, version int64) error
	Get(walletID string) (*WalletRecord, error)
}

// TxRepo persists wager transactions and resolves idempotency.
type TxRepo interface {
	Insert(tx *TxRecord) error
	FindByProviderKey(providerID, key string) (*TxRecord, error)
	FindByProviderExternal(providerID, externalID string) (*TxRecord, error)
	FindDependents(providerID, referenceExternalID string) ([]*TxRecord, error)
	Get(id string) (*TxRecord, error)
	UpdateState(tx *TxRecord) error
	MarkReversed(targetID, byKind, byTxID string) error
	ClearReversal(targetID string) error
	HasActiveWin(betTxID string) (bool, error)
}

// LedgerRepo appends entries and serves cursor pagination.
type LedgerRepo interface {
	Append(e *LedgerRecord) error
	Page(walletID string, afterVersion int64, limit int) ([]*LedgerRecord, error)
	AllOrdered(walletID string) ([]*LedgerRecord, error)
}

// AuxRepo covers inbox, outbox and work_items inside the same transaction.
type AuxRepo interface {
	InsertInbox(consumer, messageID, hash string) error
	CompleteInbox(consumer, messageID string) error
	FindInbox(consumer, messageID string) (hash string, concluded bool, found bool, err error)
	EnqueueOutbox(e *OutboxRecord) error
	ListUnpublished(aggregateID string) ([]*OutboxRecord, error)
	ClaimOutbox(owner string, leaseTTL time.Duration, limit int) ([]*OutboxRecord, error)
	MarkOutboxPublished(eventID string) error
	EnqueueWork(w *WorkItem) error
	GetWork(txID string) (*WorkItem, error)
	ClaimWork(owner string, leaseTTL time.Duration, limit int) ([]*WorkItem, error)
	TouchWork(txID string, refAttempts, infraAttempts int, nextAttemptAt time.Time, clearLease bool) error
	DeleteWork(txID string) error
}

// DB is the transactional boundary shared by all repositories.
type DB interface {
	Wallet() WalletRepo
	Tx() TxRepo
	Ledger() LedgerRepo
	Aux() AuxRepo
	// Transact runs fn inside a single SQL transaction, committing on nil.
	Transact(fn func(DB) error) error
	// TransactRepeatableRead is Transact with REPEATABLE READ isolation,
	// used by reconciliation snapshots.
	TransactRepeatableRead(fn func(DB) error) error
}
