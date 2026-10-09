// SPDX-License-Identifier: AGPL-3.0-only
package main

import (
	"context"
	"flag"
	"fmt"
	f "github.com/adamjlow/wire-server/go/services/federator"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	file := flag.String("c", "/etc/wire/federator/conf/federator.yaml", "configuration file")
	long := flag.String("config-file", "", "configuration file")
	flag.Parse()
	if *long != "" {
		*file = *long
	}
	if flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "unexpected arguments")
		os.Exit(2)
	}
	config, err := f.ReadConfig(*file)
	if err == nil {
		var service *f.Service
		service, err = f.New(config)
		if err == nil {
			ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
			defer cancel()
			err = service.Run(ctx)
		}
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "federator startup or runtime failed")
		os.Exit(1)
	}
}
