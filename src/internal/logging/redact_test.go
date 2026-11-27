package logging

import (
	"strings"
	"testing"
)

func TestRedactRegisteredSecret(t *testing.T) {
	const secretValue = "super-secret-value-123"
	RegisterSecret(secretValue)
	got := Redact("connect password=" + secretValue + " done")
	if strings.Contains(got, secretValue) {
		t.Fatalf("Redact left the secret in %q", got)
	}
	if !strings.Contains(got, "[REDACTED]") {
		t.Fatalf("Redact did not mark the value: %q", got)
	}
}

func TestRedactURLUserInfo(t *testing.T) {
	got := Redact("postgres://alice:hunter2@127.0.0.1:5432/db")
	if strings.Contains(got, "hunter2") || strings.Contains(got, "alice") {
		t.Fatalf("Redact left credentials in %q", got)
	}
	if !strings.Contains(got, "postgres://[REDACTED]@127.0.0.1:5432/db") {
		t.Fatalf("unexpected redacted URL: %q", got)
	}
}

func TestRegisterSecretIgnoresShortValues(t *testing.T) {
	RegisterSecret("abc")
	if got := Redact("abc"); got != "abc" {
		t.Fatalf("short value was redacted: %q", got)
	}
}

func TestSensitiveKey(t *testing.T) {
	for _, key := range []string{"password", "DB_PASSWORD", "api_token", "Authorization", "private_key", "cookie"} {
		if !sensitiveKey(key) {
			t.Errorf("sensitiveKey(%q) = false", key)
		}
	}
	for _, key := range []string{"username", "host", "database", "port"} {
		if sensitiveKey(key) {
			t.Errorf("sensitiveKey(%q) = true", key)
		}
	}
}
