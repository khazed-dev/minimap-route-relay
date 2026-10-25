package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadDotEnvMissingFile(t *testing.T) {
	loaded, err := loadDotEnv(filepath.Join(t.TempDir(), "missing.env"))
	if err != nil || loaded {
		t.Fatalf("missing .env should be ignored; loaded=%v, err=%v", loaded, err)
	}
}

func TestLoadDotEnvSystemEnvironmentWins(t *testing.T) {
	t.Setenv("MINIMAP_RELAY_TOKEN", "system-token-value-that-is-at-least-32-characters")
	t.Setenv("SERVER_PORT", "43000")
	t.Setenv("MINIMAP_ROUTE_TTL_SECONDS", "")
	unsetForTest(t, "MINIMAP_TEST_QUOTED")
	unsetForTest(t, "MINIMAP_TEST_SINGLE")

	path := filepath.Join(t.TempDir(), ".env")
	body := "MINIMAP_RELAY_TOKEN=dotenv-token-value-that-is-at-least-32-chars\n" +
		"SERVER_PORT=35003\n" +
		"MINIMAP_ROUTE_TTL_SECONDS=600\n" +
		"MINIMAP_TEST_QUOTED=\"walk route #1\" # inline comment\n" +
		"MINIMAP_TEST_SINGLE='literal value'\n"
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal("write test .env")
	}

	loaded, err := loadDotEnv(path)
	if err != nil || !loaded {
		t.Fatalf("loadDotEnv() loaded=%v, err=%v", loaded, err)
	}
	if os.Getenv("MINIMAP_RELAY_TOKEN") != "system-token-value-that-is-at-least-32-characters" {
		t.Fatal("system token was overwritten")
	}
	if os.Getenv("SERVER_PORT") != "43000" {
		t.Fatal("system port was overwritten")
	}
	if os.Getenv("MINIMAP_ROUTE_TTL_SECONDS") != "" {
		t.Fatal("an explicitly set empty environment variable was overwritten")
	}
	if os.Getenv("MINIMAP_TEST_QUOTED") != "walk route #1" || os.Getenv("MINIMAP_TEST_SINGLE") != "literal value" {
		t.Fatal("quoted .env values were not parsed correctly")
	}
}

func TestLoadDotEnvErrorsDoNotExposeValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte("MINIMAP_RELAY_TOKEN contains-secret-value\n"), 0600); err != nil {
		t.Fatal("write malformed test .env")
	}
	_, err := loadDotEnv(path)
	if err == nil || strings.Contains(err.Error(), "contains-secret-value") {
		t.Fatal("malformed .env error was missing or exposed its value")
	}
}

func unsetForTest(t *testing.T, key string) {
	t.Helper()
	value, wasSet := os.LookupEnv(key)
	if err := os.Unsetenv(key); err != nil {
		t.Fatal("unset test environment variable")
	}
	t.Cleanup(func() {
		if wasSet {
			_ = os.Setenv(key, value)
		} else {
			_ = os.Unsetenv(key)
		}
	})
}

func TestConfiguredPort(t *testing.T) {
	t.Run("Botkeep port takes precedence", func(t *testing.T) {
		t.Setenv("SERVER_PORT", "35003")
		t.Setenv("PORT", "8080")
		if got := configuredPort(); got != "35003" {
			t.Fatalf("configuredPort() = %q, want 35003", got)
		}
	})
	t.Run("PORT fallback", func(t *testing.T) {
		t.Setenv("SERVER_PORT", "")
		t.Setenv("PORT", "8081")
		if got := configuredPort(); got != "8081" {
			t.Fatalf("configuredPort() = %q, want 8081", got)
		}
	})
	t.Run("default port", func(t *testing.T) {
		t.Setenv("SERVER_PORT", "")
		t.Setenv("PORT", "")
		if got := configuredPort(); got != "8080" {
			t.Fatalf("configuredPort() = %q, want 8080", got)
		}
	})
}
