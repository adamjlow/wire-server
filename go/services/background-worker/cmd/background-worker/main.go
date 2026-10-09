// SPDX-License-Identifier: AGPL-3.0-only
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	worker "github.com/adamjlow/wire-server/go/services/background-worker"
)

func main() {
	config := "/etc/wire/background-worker/conf/background-worker.yaml"
	flag.StringVar(&config, "c", config, "Config file to load")
	flag.StringVar(&config, "config-file", config, "Config file to load")
	galley := flag.String("galley-config-file", "/etc/wire/galley/conf/galley.yaml", "Galley config file to load")
	flag.Parse()
	if flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "unexpected arguments")
		os.Exit(2)
	}
	c, g, err := worker.LoadConfig(config, *galley)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err = worker.New(c, g).Run(ctx); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
