package secret

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenCreatesAPIToken(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secrets.json")
	store, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	token := store.APIToken()
	if len(token) != 64 {
		t.Fatalf("api token length = %d, want 64", len(token))
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var file File
	if err := json.Unmarshal(raw, &file); err != nil {
		t.Fatalf("decode secrets file: %v", err)
	}
	if file.APIToken != token {
		t.Fatalf("persisted token does not match")
	}
	read, err := ReadToken(path)
	if err != nil {
		t.Fatalf("ReadToken: %v", err)
	}
	if read != token {
		t.Fatalf("ReadToken = %q, want %q", read, token)
	}
}

func TestSetGetDelete(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secrets.json")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Set("ws:abc:db", "correct-horse-battery"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	value, ok := store.Get("ws:abc:db")
	if !ok || value != "correct-horse-battery" {
		t.Fatalf("Get = %q, %v", value, ok)
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if value, ok := reopened.Get("ws:abc:db"); !ok || value != "correct-horse-battery" {
		t.Fatalf("persisted Get = %q, %v", value, ok)
	}

	if err := store.Delete("ws:abc:db"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, ok := store.Get("ws:abc:db"); ok {
		t.Fatal("secret still present after Delete")
	}
}

func TestRandomPasswordsAreUnique(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 64; i++ {
		value, err := RandomPassword(24)
		if err != nil {
			t.Fatal(err)
		}
		if len(value) < 16 {
			t.Fatalf("password too short: %q", value)
		}
		if strings.ContainsAny(value, "+/=") {
			t.Fatalf("password is not URL-safe: %q", value)
		}
		if seen[value] {
			t.Fatalf("duplicate password %q", value)
		}
		seen[value] = true
	}
}
