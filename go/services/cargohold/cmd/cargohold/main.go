package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	cargo "github.com/adamjlow/wire-server/go/services/cargohold"
)

func main() {
	file := flag.String("config-file", "/etc/wire/cargohold/conf/cargohold.yaml", "configuration file")
	flag.StringVar(file, "c", "/etc/wire/cargohold/conf/cargohold.yaml", "configuration file")
	flag.Parse()
	c, err := cargo.LoadConfig(*file)
	if err != nil {
		log.Fatal(err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	store, err := cargo.NewS3Store(ctx, c)
	if err != nil {
		log.Fatal("S3 initialization failed")
	}
	app := cargo.NewServer(c, store)
	server := &http.Server{Addr: net.JoinHostPort(c.Cargohold.Host, strconv.Itoa(c.Cargohold.Port)), Handler: app, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		app.StopAccepting()
		shutdown, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	app.Listening(server.Addr)
	if err = server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}
