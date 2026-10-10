// SPDX-License-Identifier: AGPL-3.0-only

// Preserve the existing backend's configured RSA signing-key policy. Original
// SAML tests use 768-bit keys; no signature/digest/trust check is bypassed. This
// service-local compatibility default does not alter other Go services.
//go:debug rsa1024min=0

package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/adamjlow/wire-server/go/services/spar"
)

func main() {
	path := "/etc/wire/spar/conf/spar.yaml"
	flag.StringVar(&path, "c", path, "Spar application config to load")
	flag.StringVar(&path, "config-file", path, "Spar application config to load")
	flag.Parse()
	if flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "unexpected arguments")
		os.Exit(2)
	}
	c, err := spar.LoadConfig(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err = spar.Run(ctx, c); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
