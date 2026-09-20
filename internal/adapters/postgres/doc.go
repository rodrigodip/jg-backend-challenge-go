package postgres

// Package postgres holds GORM-based persistence with explicit transactions,
// locks and constraints. GORM is only an executor: no AutoMigrate,
// no gorm.Model, no Save on immutable rows.
