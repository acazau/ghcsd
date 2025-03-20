// cmd/server/main.go
package main

import (
	"flag"
	"log"
	"net/http"

	"github.com/temp/ghcsd/internal/config"
	"github.com/temp/ghcsd/internal/copilot"
	"github.com/temp/ghcsd/internal/proxy"
)

func main() {
	// Parse command line flags
	debug := flag.Bool("debug", false, "Enable debug logging")
	flag.Parse()

	if *debug {
		log.Println("Debug mode enabled")
	}

	// Load configuration
	cfg, err := config.New()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Initialize auth manager and get Copilot token
	log.Println("Obtaining Copilot token...")
	authManager := copilot.NewAuthManager(&http.Client{}, cfg.ConfigDir, *debug)
	accessToken, err := authManager.GetCopilotToken()
	if err != nil {
		log.Fatalf("Failed to get copilot token: %v", err)
	}
	log.Println("Successfully obtained Copilot token")

	// Create and configure the proxy handler
	handler, err := proxy.NewHandler(accessToken, cfg.Model, *debug)
	if err != nil {
		log.Fatalf("Failed to create proxy handler: %v", err)
	}

	// Configure the server
	server := &http.Server{
		Addr:    cfg.ServerAddr,
		Handler: handler,
	}

	log.Printf("Starting server on %s", cfg.ServerAddr)
	if err := server.ListenAndServe(); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}
