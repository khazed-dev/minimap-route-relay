package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const defaultRouteTTL = 5 * time.Minute

func main() {
	token := strings.TrimSpace(os.Getenv("MINIMAP_RELAY_TOKEN"))
	if len(token) < 32 {
		log.Fatal("MINIMAP_RELAY_TOKEN must contain at least 32 characters")
	}

	port := strings.TrimSpace(os.Getenv("PORT"))
	if port == "" {
		port = "8080"
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 1 || portNumber > 65535 {
		log.Fatal("PORT must be a number between 1 and 65535")
	}

	ttl := defaultRouteTTL
	if rawTTL := strings.TrimSpace(os.Getenv("MINIMAP_ROUTE_TTL_SECONDS")); rawTTL != "" {
		seconds, parseErr := strconv.Atoi(rawTTL)
		if parseErr != nil || seconds < 60 || seconds > 3600 {
			log.Fatal("MINIMAP_ROUTE_TTL_SECONDS must be between 60 and 3600")
		}
		ttl = time.Duration(seconds) * time.Second
	}

	server := &http.Server{
		Addr:              ":" + port,
		Handler:           newHandler(token, newRouteStore(ttl)),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    8 * 1024,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	log.Printf("MiniMap route relay listening on :%s", port)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}
