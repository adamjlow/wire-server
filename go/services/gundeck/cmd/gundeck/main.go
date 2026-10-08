package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/adamjlow/wire-server/go/services/gundeck"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	path := "/etc/wire/gundeck/conf/gundeck.yaml"
	flag.StringVar(&path, "config-file", path, "configuration path (default /etc/wire/gundeck/conf/gundeck.yaml)")
	flag.StringVar(&path, "c", path, "configuration path")
	flag.Parse()
	c, err := gundeck.LoadConfig(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	service, err := gundeck.NewServer(c)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer service.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	err = service.Ready(ctx)
	cancel()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	server := &http.Server{Addr: c.Gundeck.Address(), Handler: service, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 90 * time.Second}
	stopped := make(chan os.Signal, 1)
	signal.Notify(stopped, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(stopped)
	done := make(chan error, 1)
	go func() { done <- server.ListenAndServe() }()
	select {
	case err = <-done:
		if err != http.ErrServerClosed {
			fmt.Fprintln(os.Stderr, "HTTP startup failed")
			os.Exit(1)
		}
	case <-stopped:
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if server.Shutdown(ctx) != nil {
			_ = server.Close()
		}
	}
}
