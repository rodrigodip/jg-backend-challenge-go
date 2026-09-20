package postgres

import (
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jg-backend-challenge/wallet/internal/ports"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Store is the ports.DB implementation bound to a *gorm.DB handle or a
// transaction. Repositories share the same handle so every operation in a
// Transact call commits atomically.
type Store struct {
	db *gorm.DB
}

// Open connects with the pgx-based GORM driver. Migrations are owned by the
// goose migrate job; Open never migrates.
func Open(dsn string) (*Store, error) {
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		SkipDefaultTransaction: true,
	})
	if err != nil {
		return nil, err
	}
	return &Store{db: db}, nil
}

// Transact runs fn in a single SQL transaction.
func (s *Store) Transact(fn func(ports.DB) error) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		return fn(&Store{db: tx})
	})
}

// TransactRepeatableRead runs fn with REPEATABLE READ isolation for
// consistent reconciliation snapshots. SET TRANSACTION must be the first
// statement, so it runs before fn touches any table.
func (s *Store) TransactRepeatableRead(fn func(ports.DB) error) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SET TRANSACTION ISOLATION LEVEL REPEATABLE READ").Error; err != nil {
			return err
		}
		return fn(&Store{db: tx})
	})
}

// Wallet returns the wallet repository on this handle.
func (s *Store) Wallet() ports.WalletRepo { return &walletRepo{db: s.db} }

// Tx returns the transaction repository on this handle.
func (s *Store) Tx() ports.TxRepo { return &txRepo{db: s.db} }

// Ledger returns the ledger repository on this handle.
func (s *Store) Ledger() ports.LedgerRepo { return &ledgerRepo{db: s.db} }

// Aux returns the inbox/outbox/work repository on this handle.
func (s *Store) Aux() ports.AuxRepo { return &auxRepo{db: s.db} }

// isInvalidInput reports Postgres 22P02 (invalid text representation, e.g.
// a non-UUID string bound to a uuid PK) through GORM/pgx wrapping. Reads
// with malformed ids are treated as absent, never as infra failures.
func isInvalidInput(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "22P02"
	}
	return false
}

// lockUpdate is SELECT ... FOR UPDATE on the caller's query.
func lockUpdate(db *gorm.DB) *gorm.DB {
	return db.Clauses(clause.Locking{Strength: "UPDATE"})
}

// lockUpdateSkipLocked is SELECT ... FOR UPDATE SKIP LOCKED for worker claims.
func lockUpdateSkipLocked(db *gorm.DB) *gorm.DB {
	return db.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"})
}
