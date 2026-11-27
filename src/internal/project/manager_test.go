package project

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/hxx/hxxDiscourseDevKit/internal/config"
	"github.com/hxx/hxxDiscourseDevKit/internal/workspace"
)

func TestSourcePresence(t *testing.T) {
	root := t.TempDir()

	missing := filepath.Join(root, "missing")
	if installed, gitRepo := sourcePresence(missing); installed || gitRepo {
		t.Fatalf("missing dir = (%v, %v), want (false, false)", installed, gitRepo)
	}

	empty := filepath.Join(root, "empty")
	if err := os.Mkdir(empty, 0o755); err != nil {
		t.Fatal(err)
	}
	if installed, gitRepo := sourcePresence(empty); installed || gitRepo {
		t.Fatalf("empty dir = (%v, %v), want (false, false)", installed, gitRepo)
	}

	plain := filepath.Join(root, "plain")
	if err := os.Mkdir(plain, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(plain, "README.md"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if installed, gitRepo := sourcePresence(plain); !installed || gitRepo {
		t.Fatalf("plain dir = (%v, %v), want (true, false)", installed, gitRepo)
	}

	repo := filepath.Join(root, "repo")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if installed, gitRepo := sourcePresence(repo); !installed || !gitRepo {
		t.Fatalf("git dir = (%v, %v), want (true, true)", installed, gitRepo)
	}
}

func TestRequireGitSourceUsesCheckoutOnDisk(t *testing.T) {
	paths := config.NewPaths(t.TempDir())
	m := &Manager{paths: paths}

	repoDir := filepath.Join(paths.WorkspaceDir("repo"), "source")
	if err := os.MkdirAll(filepath.Join(repoDir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	repo := &workspace.Workspace{WorkspaceID: "repo", Source: workspace.SourceConfig{Path: "source"}}
	if err := m.requireGitSource(repo); err != nil {
		t.Fatalf("checkout without persisted flag: %v", err)
	}

	emptyDir := filepath.Join(paths.WorkspaceDir("empty"), "source")
	if err := os.MkdirAll(emptyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	empty := &workspace.Workspace{WorkspaceID: "empty", Source: workspace.SourceConfig{Path: "source", Installed: true}}
	if err := m.requireGitSource(empty); !errors.Is(err, ErrSourceMissing) {
		t.Fatalf("empty dir error = %v, want ErrSourceMissing", err)
	}

	plainDir := filepath.Join(paths.WorkspaceDir("plain"), "source")
	if err := os.MkdirAll(plainDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(plainDir, "README.md"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	plain := &workspace.Workspace{WorkspaceID: "plain", Source: workspace.SourceConfig{Path: "source", Installed: true}}
	if err := m.requireGitSource(plain); !errors.Is(err, ErrNotGitRepository) {
		t.Fatalf("plain dir error = %v, want ErrNotGitRepository", err)
	}

	missing := &workspace.Workspace{WorkspaceID: "missing", Source: workspace.SourceConfig{Path: "source", Installed: true}}
	if err := m.requireGitSource(missing); !errors.Is(err, ErrSourceMissing) {
		t.Fatalf("missing dir error = %v, want ErrSourceMissing", err)
	}
}

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
