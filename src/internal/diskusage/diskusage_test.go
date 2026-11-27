package diskusage

import (
	"math"
	"testing"
)

func TestParseDF(t *testing.T) {
	output := "Filesystem     1024-blocks    Used Available Capacity Mounted on\n" +
		"/dev/sdd        1055762868 2934084 999125312       1% /\n"
	total, used, free, err := parseDF(output)
	if err != nil {
		t.Fatalf("parseDF: %v", err)
	}
	wantTotal := uint64(1055762868) * 1024
	wantUsed := uint64(2934084) * 1024
	wantFree := uint64(999125312) * 1024
	if total != wantTotal || used != wantUsed || free != wantFree {
		t.Fatalf("parseDF = (%d, %d, %d), want (%d, %d, %d)", total, used, free, wantTotal, wantUsed, wantFree)
	}
}

func TestParseDFRejectsMalformedOutput(t *testing.T) {
	cases := []string{
		"",
		"Filesystem 1024-blocks Used Available Capacity Mounted on",
		"Filesystem 1024-blocks Used Available Capacity Mounted on\n/dev/sdd not-a-number 1 1 1% /",
	}
	for _, output := range cases {
		if _, _, _, err := parseDF(output); err == nil {
			t.Fatalf("parseDF(%q) succeeded, want error", output)
		}
	}
}

func TestPercent(t *testing.T) {
	if got := percent(0, 0); got != 0 {
		t.Fatalf("percent(0, 0) = %v", got)
	}
	if got := percent(1, 3); math.Abs(got-33.33) > 0.001 {
		t.Fatalf("percent(1, 3) = %v", got)
	}
}
