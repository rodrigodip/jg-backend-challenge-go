package main

import (
	"database/sql"
	"flag"
	"fmt"
	"os"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

// migrate applies or reverts versioned migrations. It runs as a separate
// Compose job so application replicas never race schema changes.
func main() {
	dir := flag.String("dir", "migrations", "migrations directory")
	command := flag.String("command", "up", "goose command: up, down, down-to, status, version")
	args := flag.String("args", "", "extra goose args, e.g. -args 0 for \"down-to 0\"")
	flag.Parse()

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		fmt.Fprintln(os.Stderr, "DATABASE_URL is required")
		os.Exit(1)
	}
	if err := goose.SetDialect("postgres"); err != nil {
		fmt.Fprintln(os.Stderr, "invalid dialect:", err)
		os.Exit(1)
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		fmt.Fprintln(os.Stderr, "open database:", err)
		os.Exit(1)
	}
	defer db.Close()
	var extra []string
	if *args != "" {
		extra = append(extra, *args)
	}
	if err := goose.Run(*command, db, *dir, extra...); err != nil {
		fmt.Fprintln(os.Stderr, "migrate:", err)
		os.Exit(1)
	}
}
