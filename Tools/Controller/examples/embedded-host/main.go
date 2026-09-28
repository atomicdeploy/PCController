// Command embedded-host is a minimal real consumer of the public host package.
// It is intentionally small enough to copy into another Go application.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"pccontroller.local/controller/host"
)

func main() {
	dataRoot := flag.String("data-root", "", "application-owned PCController data directory")
	httpAddress := flag.String("http", "", "optional HTTP/WebSocket listen address, for example 127.0.0.1:8787")
	noConnect := flag.Bool("no-connect", false, "start without automatic board discovery")
	flag.Parse()
	if *dataRoot == "" {
		log.Fatal("--data-root is required")
	}

	var httpOptions *host.HTTPOptions
	if *httpAddress != "" {
		httpOptions = &host.HTTPOptions{Address: *httpAddress}
	}
	logger := log.New(os.Stderr, "pccontroller: ", log.LstdFlags|log.Lmicroseconds)
	embedded, err := host.New(host.Options{
		DataRoot: *dataRoot,
		Branding: host.Branding{
			AppID:   "example.embedded-host",
			AppName: "Embedded PCController Example",
		},
		Logger:             logger,
		DisableAutoConnect: *noConnect,
		HTTP:               httpOptions,
	})
	if err != nil {
		log.Fatal(err)
	}
	ctx, cancel := signal.NotifyContext(
		context.Background(), os.Interrupt, syscall.SIGTERM,
	)
	defer cancel()
	if err := embedded.Start(ctx); err != nil {
		log.Fatal(err)
	}
	for _, endpoint := range embedded.Endpoints() {
		fmt.Printf("%s %s\n", endpoint.Transport, endpoint.Address)
	}

	var ping struct {
		OK bool `json:"ok"`
	}
	callContext, callCancel := context.WithTimeout(ctx, time.Second)
	err = embedded.Call(callContext, "controller.ping", map[string]any{}, &ping)
	callCancel()
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("in-process RPC ready: %t\n", ping.OK)

	select {
	case <-ctx.Done():
	case err := <-embedded.Errors():
		if err != nil {
			logger.Printf("background failure: %v", err)
		}
	}
	stopContext, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer stopCancel()
	if err := embedded.Stop(stopContext); err != nil {
		log.Fatal(err)
	}
}
