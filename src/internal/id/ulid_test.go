package id

import (
	"strings"
	"testing"
)

func TestNewProducesUniqueValidULIDs(t *testing.T) {
	seen := make(map[string]struct{}, 1000)
	for i := 0; i < 1000; i++ {
		value, err := New()
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		if len(value) != 26 {
			t.Fatalf("length = %d, want 26 (%q)", len(value), value)
		}
		if value != strings.ToUpper(value) {
			t.Fatalf("ULID is not uppercase: %q", value)
		}
		if !Valid(value) {
			t.Fatalf("Valid(%q) = false", value)
		}
		if _, duplicate := seen[value]; duplicate {
			t.Fatalf("duplicate ULID %q", value)
		}
		seen[value] = struct{}{}
	}
}

func TestValid(t *testing.T) {
	valid := []string{
		"01ARZ3NDEKTSV4RRFFQ69G5FAV",
		"01J00000000000000000000000",
	}
	for _, value := range valid {
		if !Valid(value) {
			t.Errorf("Valid(%q) = false, want true", value)
		}
	}

	invalid := []string{
		"",
		"0",
		strings.Repeat("0", 25),
		strings.Repeat("0", 27),
		strings.Repeat("I", 26),
		strings.Repeat("L", 26),
		strings.Repeat("O", 26),
		strings.Repeat("U", 26),
		"01ARZ3NDEKTSV4RRFFQ69G5FA-",
		"81ARZ3NDEKTSV4RRFFQ69G5FAV", // first char overflows 3 timestamp bits
	}
	for _, value := range invalid {
		if Valid(value) {
			t.Errorf("Valid(%q) = true, want false", value)
		}
	}
}
