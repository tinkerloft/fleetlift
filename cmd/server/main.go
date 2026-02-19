// Package main is the Fleetlift API server entry point.
package main

import (
	"fmt"
	"log"
	"net/http"
	"os"

	flclient "github.com/tinkerloft/fleetlift/internal/client"
	"github.com/tinkerloft/fleetlift/internal/server"
)

func main() {
	c, err := flclient.NewClient()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to connect to Temporal: %v\n", err)
		os.Exit(1)
	}
	defer c.Close()

	addr := os.Getenv("FLEETLIFT_SERVER_ADDR")
	if addr == "" {
		addr = ":8080"
	}

	s := server.New(c, nil) // staticFS wired in Task 15
	log.Printf("Fleetlift server listening on %s", addr)
	if err := http.ListenAndServe(addr, s); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
