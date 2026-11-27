package workspace

import (
	"strings"
	"testing"
)

func TestTestDatabaseName(t *testing.T) {
	cases := []struct {
		name string
		base string
		want string
	}{
		{name: "normal", base: "hxxddk_12345678_demo", want: "hxxddk_12345678_demo_test"},
		{name: "empty", base: "  ", want: ""},
		{
			name: "postgres limit",
			base: strings.Repeat("a", 63),
			want: strings.Repeat("a", 58) + "_test",
		},
		{
			name: "trailing underscore removed when truncated",
			base: strings.Repeat("a", 57) + "_" + strings.Repeat("b", 5),
			want: strings.Repeat("a", 57) + "_test",
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			ws := &Workspace{Database: DatabaseConfig{DatabaseName: test.base}}
			got := ws.TestDatabaseName()
			if got != test.want {
				t.Fatalf("TestDatabaseName() = %q, want %q", got, test.want)
			}
			if len(got) > 63 {
				t.Fatalf("test database identifier length = %d, want <= 63", len(got))
			}
		})
	}
}
