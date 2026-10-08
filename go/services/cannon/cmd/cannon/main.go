package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	cannon "github.com/adamjlow/wire-server/go/services/cannon"
)

func main() {
	flag.CommandLine.SetOutput(os.Stdout)
	file := flag.String("config-file", "/etc/wire/cannon/conf/cannon.yaml", "configuration file")
	flag.StringVar(file, "c", "/etc/wire/cannon/conf/cannon.yaml", "configuration file")
	flag.Parse()
	config, err := cannon.LoadConfig(*file)
	if err != nil {
		log.Fatal(err)
	}
	backends, err := cannon.Connect(config)
	if err != nil {
		log.Fatal(err)
	}
	defer backends.Close()
	app := cannon.NewServer(config)
	app.AttachBackends(backends)
	server := &http.Server{Addr: config.Cannon.Address(), Handler: app, ReadHeaderTimeout: 10 * time.Second, ConnState: app.ConnectionState}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go app.CollectMetrics(ctx)
	app.Listening(server.Addr)
	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		<-ctx.Done()
		app.Drain()
		shutdown, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	if err = server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal("HTTP listener failed")
	}
	if ctx.Err() != nil {
		<-shutdownDone
	}
}
