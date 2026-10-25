package main

import "testing"

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
