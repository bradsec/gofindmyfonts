package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/bradsec/gofindmyfonts/internal/app"
	"github.com/bradsec/gofindmyfonts/internal/browser"
	"github.com/bradsec/gofindmyfonts/internal/logging"
)

func showBanner() {
	banner := `

  ██████╗  ██████╗     ███████╗██╗███╗   ██╗██████╗                  
 ██╔════╝ ██╔═══██╗    ██╔════╝██║████╗  ██║██╔══██╗                 
 ██║  ███╗██║   ██║    █████╗  ██║██╔██╗ ██║██║  ██║                 
 ██║   ██║██║   ██║    ██╔══╝  ██║██║╚██╗██║██║  ██║                 
 ╚██████╔╝╚██████╔╝    ██║     ██║██║ ╚████║██████╔╝                 
  ╚═════╝  ╚═════╝     ╚═╝     ╚═╝╚═╝  ╚═══╝╚═════╝                  
																		
 ███╗   ███╗██╗   ██╗    ███████╗ ██████╗ ███╗   ██╗████████╗███████╗
 ████╗ ████║╚██╗ ██╔╝    ██╔════╝██╔═══██╗████╗  ██║╚══██╔══╝██╔════╝
 ██╔████╔██║ ╚████╔╝     █████╗  ██║   ██║██╔██╗ ██║   ██║   ███████╗
 ██║╚██╔╝██║  ╚██╔╝      ██╔══╝  ██║   ██║██║╚██╗██║   ██║   ╚════██║
 ██║ ╚═╝ ██║   ██║       ██║     ╚██████╔╝██║ ╚████║   ██║   ███████║
 ╚═╝     ╚═╝   ╚═╝       ╚═╝      ╚═════╝ ╚═╝  ╚═══╝   ╚═╝   ╚══════╝
																		
`
	fmt.Println(banner)
}

func main() {
	showBanner()
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	// Load and validate configuration
	config := app.LoadConfig()
	if err := config.Validate(); err != nil {
		return fmt.Errorf("invalid configuration: %v", err)
	}

	// Initialize logging
	if err := logging.InitLogger(config.LogDir); err != nil {
		return fmt.Errorf("failed to initialize logger: %v", err)
	}

	// Create context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Initialize cleanup manager
	cleanup := app.NewCleanupManager(config)
	cleanup.ScheduleCleanup(ctx)

	// Initialize generator with config
	generator := app.NewPreviewGenerator(config)
	defer generator.Close()

	// Create and start server
	server := app.NewServer(generator)

	// Handle shutdown signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Start server in goroutine
	errChan := make(chan error, 1)
	go func() {
		logging.Info("Server starting", "server_start", "")
		errChan <- server.Start()
	}()

	// Open browser after delay
	go func() {
		time.Sleep(500 * time.Millisecond)
		url := fmt.Sprintf("http://localhost:%s", config.Port)
		if err := browser.OpenBrowser(url); err != nil {
			logging.Error("Failed to open browser", "browser_open", "", err)
		}
	}()

	// Wait for shutdown signal or error
	select {
	case err := <-errChan:
		return fmt.Errorf("server error: %v", err)
	case sig := <-sigChan:
		logging.Info(fmt.Sprintf("Received signal %v, shutting down", sig), "shutdown", "")
		cancel()
		// Allow cleanup goroutines to finish (with timeout)
		time.Sleep(2 * time.Second)
	}

	return nil
}
