// SPDX-License-Identifier: AGPL-3.0-only
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/adamjlow/wire-server/go/services/brig"
)

func main() { os.Exit(execute(os.Args[1:], os.Stdout, os.Stderr)) }

func execute(args []string, stdout, stderr io.Writer) int {
	path := "/etc/wire/brig/conf/brig.yaml"
	global := flag.NewFlagSet("brig", flag.ContinueOnError)
	global.SetOutput(stderr)
	global.StringVar(&path, "c", path, "Config file to load")
	global.StringVar(&path, "config-file", path, "Config file to load")
	global.Usage = func() {
		fmt.Fprintln(global.Output(), "Brig - User Service\nUsage: brig [-c CONFIG] [run | migrate-postgres]\nCommands: run, migrate-postgres")
		global.PrintDefaults()
	}
	for _, arg := range args {
		if arg == "--help" || arg == "-h" {
			global.SetOutput(stdout)
			break
		}
	}
	if err := global.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 1
	}
	command := "run"
	remaining := global.Args()
	if len(remaining) > 0 {
		command = remaining[0]
		remaining = remaining[1:]
	}
	if command != "run" && command != "migrate-postgres" {
		fmt.Fprintln(stderr, "invalid command")
		return 1
	}
	sub := flag.NewFlagSet(command, flag.ContinueOnError)
	sub.SetOutput(stderr)
	sub.StringVar(&path, "c", path, "Config file to load")
	sub.StringVar(&path, "config-file", path, "Config file to load")
	dbname := ""
	reset := false
	if command == "migrate-postgres" {
		sub.StringVar(&dbname, "dbname", "", "overrides the dbname from brig config")
		sub.BoolVar(&reset, "reset", false, "WARNING: delete all public schema data; never use in production")
	}
	sub.Usage = func() { fmt.Fprintln(sub.Output(), "Usage: brig "+command+" [OPTIONS]"); sub.PrintDefaults() }
	for _, arg := range remaining {
		if arg == "--help" || arg == "-h" {
			sub.SetOutput(stdout)
		}
		if strings.HasPrefix(arg, "--reset=") {
			fmt.Fprintln(stderr, "invalid reset option")
			return 1
		}
	}
	if err := sub.Parse(remaining); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 1
	}
	if sub.NArg() != 0 {
		fmt.Fprintln(stderr, "unexpected arguments")
		return 1
	}
	c, err := brig.LoadConfig(path)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if command == "migrate-postgres" {
		ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
		defer stop()
		if err = brig.MigratePostgres(ctx, c, dbname, reset); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		return 0
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err = brig.Run(ctx, c); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}
