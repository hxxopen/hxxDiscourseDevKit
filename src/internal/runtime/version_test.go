package runtime

import "testing"

func TestExtractVersion(t *testing.T) {
	cases := map[string]string{
		"psql (PostgreSQL) 16.2":       "16.2",
		"redis-server v=7.2.4":         "7.2.4",
		"ruby 3.3.1p55":                "3.3.1",
		"git version 2.45.1.windows.1": "2.45.1",
		"v20.11.0":                     "20.11.0",
		"no version here":              "",
		"版本 16":                        "",
	}
	for input, want := range cases {
		if got := ExtractVersion(input); got != want {
			t.Errorf("ExtractVersion(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestCompatible(t *testing.T) {
	cases := []struct {
		actual   string
		required string
		want     bool
		wantErr  bool
	}{
		{"16.2", "system", true, false},
		{"16.2", "*", true, false},
		{"16.2", "16.x", true, false},
		{"15.9", "16.x", false, false},
		{"16.2", "3.3", false, false},
		{"3.3.1", "3.3", true, false},
		{"16.2", ">=16.0 <17.0", true, false},
		{"17.0", ">=16.0 <17.0", false, false},
		{"1.4.0", "^1.2.0", true, false},
		{"2.0.0", "^1.2.0", false, false},
		{"1.2.9", "~1.2.3", true, false},
		{"1.3.0", "~1.2.3", false, false},
		{"16.2", ">=", false, true},
	}
	for _, tc := range cases {
		got, err := Compatible(tc.actual, tc.required)
		if (err != nil) != tc.wantErr {
			t.Errorf("Compatible(%q, %q) error = %v, wantErr %v", tc.actual, tc.required, err, tc.wantErr)
			continue
		}
		if err == nil && got != tc.want {
			t.Errorf("Compatible(%q, %q) = %v, want %v", tc.actual, tc.required, got, tc.want)
		}
	}
}
