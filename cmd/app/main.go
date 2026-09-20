package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/jg-backend-challenge/wallet/internal/app"
)

func main() {
	mode := flag.String("mode", "", "process mode: api, consumer or workers (overrides APP_MODE)")
	flag.Parse()
	if *mode != "" {
		_ = os.Setenv("APP_MODE", *mode)
	}
	cfg, err := app.LoadFromEnv()
	if err != nil {
		fmt.Fprintln(os.Stderr, "invalid configuration:", err)
		os.Exit(1)
	}
	app.New(cfg).Run()
}
