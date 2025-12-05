// cmd/relay/main.go
package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/lobber-dev/lobber/internal/db"
	"github.com/lobber-dev/lobber/internal/relay"
)

func main() {
	if err := run(); err != nil {
		log.Fatalf("error: %v", err)
	}
}

func run() error {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// Connect to database
	database, err := db.New(ctx)
	if err != nil {
		log.Printf("Warning: database connection failed, continuing without DB: %v", err)
		database = nil
	} else {
		defer database.Close()
	}

	// Create server config with Stripe settings
	config := relay.DefaultServerConfig()
	config.StripeAPIKey = os.Getenv("STRIPE_API_KEY")
	config.StripeWebhookKey = os.Getenv("STRIPE_WEBHOOK_SECRET")

	// Set up TLS
	serviceDomain := os.Getenv("SERVICE_DOMAIN")
	if serviceDomain == "" {
		serviceDomain = "lobber.dev"
	}

	config.BaseDomain = serviceDomain

	// Create server
	server := relay.NewServerWithConfig(database, config)

	cacheDir := os.Getenv("CERT_CACHE_DIR")
	if cacheDir == "" {
		cacheDir = "/var/cache/lobber/certs"
	}

	tlsMgr := relay.NewTLSManager(serviceDomain, cacheDir)

	// HTTP server for ACME challenges
	httpAddr := os.Getenv("HTTP_ADDR")
	if httpAddr == "" {
		httpAddr = ":80"
	}

	httpServer := &http.Server{
		Addr:    httpAddr,
		Handler: tlsMgr.HTTPHandler(server),
	}

	// HTTPS server
	httpsAddr := os.Getenv("HTTPS_ADDR")
	if httpsAddr == "" {
		httpsAddr = ":443"
	}

	httpsServer := &http.Server{
		Addr:    httpsAddr,
		Handler: server,
		TLSConfig: &tls.Config{
			GetCertificate: tlsMgr.GetCertificate,
			NextProtos:     []string{"h2", "http/1.1"},
		},
	}

	// Start servers
	errCh := make(chan error, 2)

	go func() {
		log.Printf("HTTP server listening on %s", httpAddr)
		if err := httpServer.ListenAndServe(); err != http.ErrServerClosed {
			errCh <- fmt.Errorf("http: %w", err)
		}
	}()

	go func() {
		log.Printf("HTTPS server listening on %s", httpsAddr)
		if err := httpsServer.ListenAndServeTLS("", ""); err != http.ErrServerClosed {
			errCh <- fmt.Errorf("https: %w", err)
		}
	}()

	// Wait for shutdown
	select {
	case <-ctx.Done():
		log.Println("Shutting down...")
	case err := <-errCh:
		return err
	}

	// Graceful shutdown
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Printf("HTTP shutdown error: %v", err)
	}
	if err := httpsServer.Shutdown(shutdownCtx); err != nil {
		log.Printf("HTTPS shutdown error: %v", err)
	}

	return nil
}
