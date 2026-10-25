package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
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
	loaded, err := loadDotEnv(".env")
	if err != nil {
		log.Fatalf("load .env: %v", err)
	}
	if loaded {
		log.Printf("Loaded .env from working directory")
	}

	token := strings.TrimSpace(os.Getenv("MINIMAP_RELAY_TOKEN"))
	if len(token) < 32 {
		log.Fatal("MINIMAP_RELAY_TOKEN must contain at least 32 characters")
	}

	port := configuredPort()
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 1 || portNumber > 65535 {
		log.Fatal("SERVER_PORT/PORT must be a number between 1 and 65535")
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

// loadDotEnv reads KEY=VALUE entries without overriding variables already
// supplied by the process environment. Values are never logged.
func loadDotEnv(path string) (bool, error) {
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("open %s: %w", path, err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSpace(strings.TrimPrefix(scanner.Text(), "\uFEFF"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		key, rawValue, found := strings.Cut(line, "=")
		key = strings.TrimSpace(key)
		if !found || !validDotEnvKey(key) {
			return false, fmt.Errorf("%s:%d: expected KEY=VALUE", path, lineNumber)
		}
		value, err := parseDotEnvValue(strings.TrimSpace(rawValue))
		if err != nil {
			return false, fmt.Errorf("%s:%d: invalid value for %s", path, lineNumber, key)
		}
		if _, exists := os.LookupEnv(key); exists {
			continue
		}
		if err := os.Setenv(key, value); err != nil {
			return false, fmt.Errorf("%s:%d: unable to set %s", path, lineNumber, key)
		}
	}
	if err := scanner.Err(); err != nil {
		return false, fmt.Errorf("read %s: %w", path, err)
	}
	return true, nil
}

func validDotEnvKey(key string) bool {
	if key == "" {
		return false
	}
	for index, char := range key {
		valid := char == '_' || char >= 'A' && char <= 'Z' || char >= 'a' && char <= 'z'
		if index > 0 {
			valid = valid || char >= '0' && char <= '9'
		}
		if !valid {
			return false
		}
	}
	return true
}

func parseDotEnvValue(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", nil
	}
	quote := value[0]
	if quote == '"' || quote == '\'' {
		closing := -1
		for index := 1; index < len(value); index++ {
			if quote == '"' && value[index] == '\\' {
				index++
				continue
			}
			if value[index] == quote {
				closing = index
				break
			}
		}
		if closing < 0 {
			return "", fmt.Errorf("unterminated quoted value")
		}
		remainder := strings.TrimSpace(value[closing+1:])
		if remainder != "" && !strings.HasPrefix(remainder, "#") {
			return "", fmt.Errorf("unexpected trailing characters")
		}
		quoted := value[:closing+1]
		if quote == '\'' {
			return quoted[1 : len(quoted)-1], nil
		}
		return strconv.Unquote(quoted)
	}
	if comment := strings.Index(value, " #"); comment >= 0 {
		value = value[:comment]
	}
	return strings.TrimSpace(value), nil
}

func configuredPort() string {
	if port := strings.TrimSpace(os.Getenv("SERVER_PORT")); port != "" {
		return port
	}
	if port := strings.TrimSpace(os.Getenv("PORT")); port != "" {
		return port
	}
	return "8080"
}
