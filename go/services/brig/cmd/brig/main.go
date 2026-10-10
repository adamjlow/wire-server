// SPDX-License-Identifier: AGPL-3.0-only
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/adamjlow/wire-server/go/services/brig"
)

func main() {
	path := "/etc/wire/brig/conf/brig.yaml"
	flag.StringVar(&path, "c", path, "Config file to load")
	flag.StringVar(&path, "config-file", path, "Config file to load")
	flag.Usage = func() {
		fmt.Fprintln(flag.CommandLine.Output(), "Brig - User Service\nUsage: brig [-c CONFIG] [run | migrate-postgres [--dbname NAME] [--reset]]")
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() > 0 && (flag.NArg() != 1 || flag.Arg(0) != "run") {
		// No successful migration stub: implementing the native ledger/CLI is
		// still required before the whole replacement qualifies.
		fmt.Fprintln(os.Stderr, "command is not available in this initial Brig implementation")
		os.Exit(1)
	}
	c, err := brig.LoadConfig(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err = brig.Run(ctx, c); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
