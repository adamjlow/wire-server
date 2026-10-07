// SPDX-License-Identifier: AGPL-3.0-or-later
package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	proxy "github.com/adamjlow/wire-server/go/services/proxy"
)

func run() error {
	filename := "/etc/wire/proxy/conf/proxy.yaml"
	flag.StringVar(&filename, "c", filename, "configuration file")
	flag.StringVar(&filename, "config-file", filename, "configuration file")
	flag.Parse()
	if flag.NArg() != 0 {
		return fmt.Errorf("unexpected positional argument")
	}
	config, err := proxy.LoadConfig(filename)
	if err != nil {
		return err
	}
	secrets, err := proxy.LoadSecrets(config.SecretsConfig)
	if err != nil {
		return err
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	server, logger := proxy.NewServer(config, secrets, ctx)
	listener, err := net.Listen("tcp", server.Addr)
	if err != nil {
		return err
	}
	logger.Listening(server.Addr)
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		<-ctx.Done()
		shutdown, release := context.WithTimeout(context.Background(), 30*time.Second)
		defer release()
		if server.Shutdown(shutdown) != nil {
			_ = server.Close()
		}
	}()
	err = server.Serve(listener)
	if err == http.ErrServerClosed {
		<-stopped
		return nil
	}
	return err
}

func main() {
	if run() != nil {
		fmt.Fprintln(os.Stderr, "proxy could not start or run")
		os.Exit(1)
	}
}
