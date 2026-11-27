package project

import (
	"errors"
	"testing"
)

func TestValidateSourceRejectsCredentials(t *testing.T) {
	cases := []string{
		"https://user:password@github.com/discourse/discourse.git",
		"https://token@github.com/discourse/discourse.git",
		"ssh://git@github.com/discourse/discourse.git",
		"http://user@example.com/repo.git",
	}
	for _, value := range cases {
		err := validateSource(value, "main", "", "")
		if !errors.Is(err, ErrInvalidSource) {
			t.Errorf("validateSource(%q) error = %v, want ErrInvalidSource", value, err)
		}
	}
}

func TestValidateSourceAllowsSafeForms(t *testing.T) {
	cases := []string{
		"https://github.com/discourse/discourse.git",
		"ssh://github.com/discourse/discourse.git",
		"git://example.com/repo.git",
		"file:///srv/repos/discourse.git",
		"git@github.com:discourse/discourse.git",
	}
	for _, value := range cases {
		if err := validateSource(value, "main", "", ""); err != nil {
			t.Errorf("validateSource(%q) = %v", value, err)
		}
	}
}

func TestValidateSourceRejectsUnsafeInput(t *testing.T) {
	cases := []struct {
		url    string
		branch string
	}{
		{"--upload-pack=evil", "main"},
		{"https://exa\nmple.com/repo.git", "main"},
		{"https://example.com/repo.git", "-branch"},
		{"https://example.com/repo.git", "main..evil"},
		{"https://example.com/repo.git", "main branch"},
		{"https://example.com/repo.git", "main~1"},
		{"https://example.com/repo.git", "refs/heads/main/"},
		{"", "main"},
	}
	for _, test := range cases {
		err := validateSource(test.url, test.branch, "", "")
		if err == nil {
			t.Errorf("validateSource(%q, %q) = nil", test.url, test.branch)
		}
	}
}

func TestParsePorcelainV2(t *testing.T) {
	raw := "# branch.oid 1234567890abcdef\n" +
		"# branch.head main\n" +
		"# branch.upstream origin/main\n" +
		"# branch.ab +2 -3\n" +
		"1 .M N... 100644 100644 100644 abcdef abcdef file.txt\n" +
		"2 R. N... 100644 100644 100644 abcdef abcdef R100 new.txt\told.txt\n" +
		"? untracked.txt\n"
	dirty, changed, ahead, behind := parsePorcelainV2(raw)
	if !dirty || changed != 3 || ahead != 2 || behind != 3 {
		t.Fatalf("parsePorcelainV2 = (%v, %d, %d, %d)", dirty, changed, ahead, behind)
	}
}

func TestParsePorcelainV2Clean(t *testing.T) {
	dirty, changed, ahead, behind := parsePorcelainV2("# branch.oid abc\n# branch.head main\n")
	if dirty || changed != 0 || ahead != 0 || behind != 0 {
		t.Fatalf("clean parse = (%v, %d, %d, %d)", dirty, changed, ahead, behind)
	}
}
