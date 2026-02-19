// Package main is the Fleetlift API server entry point.
package main

import (
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"

	flclient "github.com/tinkerloft/fleetlift/internal/client"
	"github.com/tinkerloft/fleetlift/internal/server"
	flweb "github.com/tinkerloft/fleetlift/web"
)

func main() {
	c, err := flclient.NewClient()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to connect to Temporal: %v\n", err)
		os.Exit(1)
	}
	defer c.Close()

	webFS, err := fs.Sub(flweb.DistFS, "dist")
	if err != nil {
		log.Fatalf("failed to prepare static files: %v", err)
	}

	addr := os.Getenv("FLEETLIFT_SERVER_ADDR")
	if addr == "" {
		addr = ":8080"
	}

	s := server.New(c, webFS)
	log.Printf("Fleetlift server listening on %s", addr)
	if err := http.ListenAndServe(addr, s); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
