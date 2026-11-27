package project

import (
	"strings"
	"testing"
)

func TestParseEnvFile(t *testing.T) {
	data := "\uFEFF# generated\nRAILS_ENV=\"development\"\n\nDISCOURSE_DB_PASSWORD=\"p@ss\\\"word\"\nDISCOURSE_HOSTNAME=localhost\n"
	values, err := parseEnvFile(data)
	if err != nil {
		t.Fatalf("parseEnvFile: %v", err)
	}
	if len(values) != 3 {
		t.Fatalf("values = %#v", values)
	}
	if values[0].Key != "RAILS_ENV" || values[0].Value != "development" {
		t.Fatalf("RAILS_ENV = %#v", values[0])
	}
	if values[1].Value != `p@ss"word` {
		t.Fatalf("quoted value = %q", values[1].Value)
	}
	if values[2].Value != "localhost" {
		t.Fatalf("plain value = %q", values[2].Value)
	}
}

func TestParseEnvFileRejectsMalformedLines(t *testing.T) {
	if _, err := parseEnvFile("NOT_A_PAIR\n"); err == nil {
		t.Fatal("expected malformed line error")
	}
	if _, err := parseEnvFile(`RAILS_ENV="\q"`); err == nil {
		t.Fatal("expected invalid quoted value error")
	}
}

func TestValidEnvKey(t *testing.T) {
	for _, key := range []string{"RAILS_ENV", "_PRIVATE", "A1"} {
		if !validEnvKey(key) {
			t.Errorf("validEnvKey(%q) = false", key)
		}
	}
	for _, key := range []string{"", "1KEY", "KEY-NAME", "KEY NAME", "键"} {
		if validEnvKey(key) {
			t.Errorf("validEnvKey(%q) = true", key)
		}
	}
}

func TestMergeEnvironmentOverrides(t *testing.T) {
	merged := mergeEnvironment(
		[]string{"PATH=/bin", "RAILS_ENV=production"},
		[]envVar{{Key: "RAILS_ENV", Value: "development"}, {Key: "DISCOURSE_HOSTNAME", Value: "localhost"}},
	)
	joined := strings.Join(merged, "\n")
	if !strings.Contains(joined, "RAILS_ENV=development") {
		t.Fatalf("merged = %q", joined)
	}
	if strings.Contains(joined, "RAILS_ENV=production") {
		t.Fatalf("old value was kept: %q", joined)
	}
	if !strings.Contains(joined, "DISCOURSE_HOSTNAME=localhost") {
		t.Fatalf("new value missing: %q", joined)
	}
}

func TestEnvSecretValues(t *testing.T) {
	values := envSecretValues([]envVar{
		{Key: "RAILS_ENV", Value: "development"},
		{Key: "DISCOURSE_DB_PASSWORD", Value: "secret"},
		{Key: "DISCOURSE_REDIS_PASSWORD", Value: "redis-secret"},
	})
	if len(values) != 2 {
		t.Fatalf("secret values = %#v", values)
	}
}
