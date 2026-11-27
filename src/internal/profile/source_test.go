package profile

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func sampleManifest(id string) string {
	return fmt.Sprintf(`{
  "schema_version": 1,
  "name": "Acme 模板",
  "version": "1.2.0",
  "description": "Acme 内部模板来源",
  "profiles": [
    {
      "id": %q,
      "name": "Acme Discourse",
      "description": "Acme 的 Discourse 开发模板。",
      "project_type": "discourse-acme",
      "default_repository_url": "https://git.example.com/acme/discourse.git",
      "default_branch": "main",
      "source_required": true,
      "runtime_components": ["ruby", "node", "git"],
      "services": ["postgresql", "redis"],
      "operations": [
        {"id": "workspace.create", "title": "创建 Workspace", "description": "创建隔离目录。", "destructive": false}
      ],
      "permissions": [
        {"id": "workspace.write", "title": "写入实例目录", "description": "写入 workspaces/。", "risk": "low"}
      ]
    }
  ]
}`, id)
}

func writeManifest(t *testing.T, dir, body string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir source: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, ManifestFileName), []byte(body), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
}

func newSourceCatalog(t *testing.T) (*Catalog, string, string) {
	t.Helper()
	root := t.TempDir()
	storePath := filepath.Join(root, "config", "profile-sources.json")
	sourcesDir := filepath.Join(root, "profiles")
	catalog := New(healthyRuntime(), healthyServices())
	if err := catalog.EnableSources(storePath, sourcesDir, nil); err != nil {
		t.Fatalf("EnableSources: %v", err)
	}
	return catalog, storePath, sourcesDir
}

func TestAddLocalProfileSource(t *testing.T) {
	catalog, storePath, _ := newSourceCatalog(t)
	sourceDir := filepath.Join(t.TempDir(), "acme-profiles")
	writeManifest(t, sourceDir, sampleManifest("acme-discourse"))

	src, err := catalog.AddSource(context.Background(), SourceRequest{Location: sourceDir})
	if err != nil {
		t.Fatalf("AddSource: %v", err)
	}
	if src.Kind != SourceKindLocal || src.Name != "Acme 模板" || src.Version != "1.2.0" {
		t.Fatalf("source = %+v", src)
	}
	if len(src.ProfileIDs) != 1 || src.ProfileIDs[0] != "acme-discourse" {
		t.Fatalf("profile IDs = %v", src.ProfileIDs)
	}
	if src.LastLoadedAt == nil || src.LastError != "" {
		t.Fatalf("load state = %+v", src)
	}

	sources := catalog.ListSources()
	if len(sources) != 1 || sources[0].ID != src.ID {
		t.Fatalf("sources = %+v", sources)
	}
	defs := catalog.List()
	if len(defs) != 6 {
		t.Fatalf("profiles = %d, want 6", len(defs))
	}
	def, err := catalog.Get("ACME-DISCOURSE")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if def.Origin.Type != "third-party" || def.Origin.Verified || def.Origin.SourceID != src.ID {
		t.Fatalf("origin = %+v", def.Origin)
	}
	if def.Origin.URL != src.Location {
		t.Fatalf("origin URL = %q", def.Origin.URL)
	}
	if !def.SourceRequired || def.DefaultBranch != "main" {
		t.Fatalf("definition = %+v", def)
	}

	report, err := catalog.Check(context.Background(), "acme-discourse")
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !report.Ready || report.WarningCount == 0 {
		t.Fatalf("report = %+v", report)
	}
	origin := findCheck(t, report, "origin")
	if origin.Status != StatusWarning || !strings.Contains(origin.Message, "未验证") {
		t.Fatalf("origin check = %+v", origin)
	}

	if _, err := catalog.AddSource(context.Background(), SourceRequest{Location: sourceDir}); !errors.Is(err, ErrSourceExists) {
		t.Fatalf("duplicate add error = %v, want ErrSourceExists", err)
	}

	if err := catalog.RemoveSource(src.ID); err != nil {
		t.Fatalf("RemoveSource: %v", err)
	}
	if len(catalog.ListSources()) != 0 || len(catalog.List()) != 5 {
		t.Fatalf("source removal did not restore builtins: %+v", catalog.ListSources())
	}
	if _, err := catalog.Get("acme-discourse"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get removed profile error = %v", err)
	}
	if _, err := os.Stat(storePath); err != nil {
		t.Fatalf("source registry missing: %v", err)
	}

	if err := catalog.RemoveSource(src.ID); !errors.Is(err, ErrSourceNotFound) {
		t.Fatalf("second remove error = %v, want ErrSourceNotFound", err)
	}
}

func TestAddSourceValidatesManifest(t *testing.T) {
	tests := []struct {
		name     string
		manifest string
	}{
		{
			name:     "wrong schema",
			manifest: `{"schema_version":2,"name":"x","profiles":[]}`,
		},
		{
			name:     "unknown field",
			manifest: `{"schema_version":1,"name":"x","profiles":[],"extra":true}`,
		},
		{
			name:     "duplicate profile ID",
			manifest: `{"schema_version":1,"name":"x","profiles":[` + manifestProfileJSON("dup") + `,` + manifestProfileJSON("dup") + `]}`,
		},
		{
			name:     "bad profile ID",
			manifest: `{"schema_version":1,"name":"x","profiles":[` + manifestProfileJSON("Bad_ID") + `]}`,
		},
		{
			name:     "missing operations",
			manifest: `{"schema_version":1,"name":"x","profiles":[{"id":"p","name":"P","description":"D","project_type":"discourse","permissions":[{"id":"workspace.write","title":"W","description":"D","risk":"low"}]}]}`,
		},
		{
			name:     "missing permissions",
			manifest: `{"schema_version":1,"name":"x","profiles":[{"id":"p","name":"P","description":"D","project_type":"discourse","operations":[{"id":"workspace.create","title":"C","description":"D"}]}]}`,
		},
		{
			name:     "bad repository URL",
			manifest: `{"schema_version":1,"name":"x","profiles":[{"id":"p","name":"P","description":"D","project_type":"discourse","default_repository_url":"ftp://example.com/repo.git","operations":[{"id":"workspace.create","title":"C","description":"D"}],"permissions":[{"id":"workspace.write","title":"W","description":"D","risk":"low"}]}]}`,
		},
		{
			name:     "branch without repository",
			manifest: `{"schema_version":1,"name":"x","profiles":[{"id":"p","name":"P","description":"D","project_type":"discourse","default_branch":"main","operations":[{"id":"workspace.create","title":"C","description":"D"}],"permissions":[{"id":"workspace.write","title":"W","description":"D","risk":"low"}]}]}`,
		},
		{
			name:     "unknown runtime component",
			manifest: `{"schema_version":1,"name":"x","profiles":[{"id":"p","name":"P","description":"D","project_type":"discourse","runtime_components":["go"],"operations":[{"id":"workspace.create","title":"C","description":"D"}],"permissions":[{"id":"workspace.write","title":"W","description":"D","risk":"low"}]}]}`,
		},
		{
			name:     "bad permission risk",
			manifest: `{"schema_version":1,"name":"x","profiles":[{"id":"p","name":"P","description":"D","project_type":"discourse","operations":[{"id":"workspace.create","title":"C","description":"D"}],"permissions":[{"id":"workspace.write","title":"W","description":"D","risk":"extreme"}]}]}`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			catalog, _, _ := newSourceCatalog(t)
			sourceDir := filepath.Join(t.TempDir(), "source")
			writeManifest(t, sourceDir, test.manifest)
			if _, err := catalog.AddSource(context.Background(), SourceRequest{Location: sourceDir}); !errors.Is(err, ErrSourceInvalid) {
				t.Fatalf("AddSource error = %v, want ErrSourceInvalid", err)
			}
			if len(catalog.ListSources()) != 0 {
				t.Fatalf("invalid source was registered: %+v", catalog.ListSources())
			}
		})
	}
}

func manifestProfileJSON(id string) string {
	return fmt.Sprintf(`{"id":%q,"name":"P","description":"D","project_type":"discourse","operations":[{"id":"workspace.create","title":"C","description":"D"}],"permissions":[{"id":"workspace.write","title":"W","description":"D","risk":"low"}]}`, id)
}

func TestSourceProfileIDConflict(t *testing.T) {
	catalog, _, _ := newSourceCatalog(t)
	sourceDir := filepath.Join(t.TempDir(), "source")
	writeManifest(t, sourceDir, sampleManifest("discourse-official"))
	if _, err := catalog.AddSource(context.Background(), SourceRequest{Location: sourceDir}); !errors.Is(err, ErrSourceConflict) {
		t.Fatalf("AddSource error = %v, want ErrSourceConflict", err)
	}
}

func TestSourceDefaults(t *testing.T) {
	catalog, _, _ := newSourceCatalog(t)
	sourceDir := filepath.Join(t.TempDir(), "source")
	writeManifest(t, sourceDir, `{
  "schema_version": 1,
  "name": "Minimal",
  "profiles": [{
    "id": "minimal",
    "name": "Minimal",
    "description": "Minimal profile.",
    "project_type": "discourse",
    "operations": [{"id": "workspace.create", "title": "Create", "description": "Create it."}],
    "permissions": [{"id": "workspace.write", "title": "Write", "description": "Write it.", "risk": "LOW"}]
  }]
}`)
	if _, err := catalog.AddSource(context.Background(), SourceRequest{Location: sourceDir}); err != nil {
		t.Fatalf("AddSource: %v", err)
	}
	def, err := catalog.Get("minimal")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !def.SourceRequired {
		t.Fatal("source_required should default to true")
	}
	if strings.Join(def.RuntimeComponents, ",") != "ruby,node,git" {
		t.Fatalf("runtime components = %v", def.RuntimeComponents)
	}
	if strings.Join(def.Services, ",") != "postgresql,redis" {
		t.Fatalf("services = %v", def.Services)
	}
	if def.Category != "third-party" || def.Permissions[0].Risk != "low" {
		t.Fatalf("defaults = %+v", def)
	}
}

func TestSourcesPersistAndRefresh(t *testing.T) {
	catalog, storePath, sourcesDir := newSourceCatalog(t)
	sourceDir := filepath.Join(t.TempDir(), "source")
	writeManifest(t, sourceDir, sampleManifest("acme-one"))
	src, err := catalog.AddSource(context.Background(), SourceRequest{Location: sourceDir})
	if err != nil {
		t.Fatalf("AddSource: %v", err)
	}

	reloaded := New(healthyRuntime(), healthyServices())
	if err := reloaded.EnableSources(storePath, sourcesDir, nil); err != nil {
		t.Fatalf("EnableSources reload: %v", err)
	}
	if len(reloaded.ListSources()) != 1 || len(reloaded.List()) != 6 {
		t.Fatalf("reloaded sources = %+v, profiles = %d", reloaded.ListSources(), len(reloaded.List()))
	}
	if _, err := reloaded.Get("acme-one"); err != nil {
		t.Fatalf("reloaded profile: %v", err)
	}

	writeManifest(t, sourceDir, strings.ReplaceAll(sampleManifest("acme-two"), "1.2.0", "2.0.0"))
	updated, err := reloaded.RefreshSource(context.Background(), src.ID)
	if err != nil {
		t.Fatalf("RefreshSource: %v", err)
	}
	if updated.Version != "2.0.0" || len(updated.ProfileIDs) != 1 || updated.ProfileIDs[0] != "acme-two" {
		t.Fatalf("updated source = %+v", updated)
	}
	if _, err := reloaded.Get("acme-one"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("old profile still present: %v", err)
	}
	if _, err := reloaded.Get("acme-two"); err != nil {
		t.Fatalf("new profile missing: %v", err)
	}
}

func TestStartupRecordsMissingLocalSource(t *testing.T) {
	catalog, storePath, sourcesDir := newSourceCatalog(t)
	sourceDir := filepath.Join(t.TempDir(), "source")
	writeManifest(t, sourceDir, sampleManifest("acme-one"))
	if _, err := catalog.AddSource(context.Background(), SourceRequest{Location: sourceDir}); err != nil {
		t.Fatalf("AddSource: %v", err)
	}
	if err := os.RemoveAll(sourceDir); err != nil {
		t.Fatalf("remove source: %v", err)
	}

	reloaded := New(healthyRuntime(), healthyServices())
	if err := reloaded.EnableSources(storePath, sourcesDir, nil); err != nil {
		t.Fatalf("EnableSources: %v", err)
	}
	sources := reloaded.ListSources()
	if len(sources) != 1 || sources[0].LastError == "" {
		t.Fatalf("sources = %+v", sources)
	}
	if len(reloaded.List()) != 5 {
		t.Fatalf("failed source should not publish profiles: %d", len(reloaded.List()))
	}
}

func TestEnableSourcesRejectsCorruptRegistry(t *testing.T) {
	root := t.TempDir()
	storePath := filepath.Join(root, "sources.json")
	if err := os.WriteFile(storePath, []byte("{not json"), 0o600); err != nil {
		t.Fatalf("write registry: %v", err)
	}
	catalog := New(nil, nil)
	if err := catalog.EnableSources(storePath, filepath.Join(root, "profiles"), nil); err == nil {
		t.Fatal("EnableSources accepted a corrupt registry")
	}
	if catalog.SourcesEnabled() {
		t.Fatal("corrupt registry should not enable sources")
	}
}

type fakeGitFetcher struct {
	manifest []byte
	commit   string
	err      error
	calls    int
	lastURL  string
	lastRef  string
}

func (f *fakeGitFetcher) Fetch(ctx context.Context, repoURL, branch, destDir string) (string, error) {
	f.calls++
	f.lastURL = repoURL
	f.lastRef = branch
	if f.err != nil {
		return "", f.err
	}
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(destDir, ManifestFileName), f.manifest, 0o644); err != nil {
		return "", err
	}
	return f.commit, nil
}

func TestGitSourceWithFetcher(t *testing.T) {
	root := t.TempDir()
	storePath := filepath.Join(root, "config", "profile-sources.json")
	sourcesDir := filepath.Join(root, "profiles")
	fetcher := &fakeGitFetcher{
		manifest: []byte(sampleManifest("acme-git")),
		commit:   "0123456789abcdef",
	}
	catalog := New(healthyRuntime(), healthyServices())
	if err := catalog.EnableSources(storePath, sourcesDir, fetcher); err != nil {
		t.Fatalf("EnableSources: %v", err)
	}

	src, err := catalog.AddSource(context.Background(), SourceRequest{
		Location: "https://git.example.com/acme/profiles.git",
		Branch:   "main",
	})
	if err != nil {
		t.Fatalf("AddSource: %v", err)
	}
	if src.Kind != SourceKindGit || src.Commit != "0123456789abcdef" || src.Branch != "main" {
		t.Fatalf("source = %+v", src)
	}
	if fetcher.calls != 1 || fetcher.lastURL != "https://git.example.com/acme/profiles.git" || fetcher.lastRef != "main" {
		t.Fatalf("fetcher calls = %+v", fetcher)
	}
	if _, err := os.Stat(filepath.Join(sourcesDir, src.ID, ManifestFileName)); err != nil {
		t.Fatalf("checkout missing: %v", err)
	}

	fetcher.commit = "fedcba9876543210"
	updated, err := catalog.RefreshSource(context.Background(), src.ID)
	if err != nil {
		t.Fatalf("RefreshSource: %v", err)
	}
	if updated.Commit != "fedcba9876543210" || fetcher.calls != 2 {
		t.Fatalf("refreshed source = %+v, calls = %d", updated, fetcher.calls)
	}
	if err := catalog.RemoveSource(src.ID); err != nil {
		t.Fatalf("RemoveSource: %v", err)
	}
	if _, err := os.Stat(filepath.Join(sourcesDir, src.ID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("checkout was not removed: %v", err)
	}
}

func TestGitSourceFetchFailure(t *testing.T) {
	root := t.TempDir()
	fetcher := &fakeGitFetcher{err: errors.New("network down")}
	catalog := New(nil, nil)
	if err := catalog.EnableSources(filepath.Join(root, "sources.json"), filepath.Join(root, "profiles"), fetcher); err != nil {
		t.Fatalf("EnableSources: %v", err)
	}
	_, err := catalog.AddSource(context.Background(), SourceRequest{Location: "https://git.example.com/acme/profiles.git"})
	if !errors.Is(err, ErrSourceUnavailable) {
		t.Fatalf("AddSource error = %v, want ErrSourceUnavailable", err)
	}
	if len(catalog.ListSources()) != 0 {
		t.Fatal("failed source was registered")
	}
}

func TestSourcesDisabled(t *testing.T) {
	catalog := New(nil, nil)
	if catalog.SourcesEnabled() {
		t.Fatal("sources enabled before EnableSources")
	}
	if _, err := catalog.AddSource(context.Background(), SourceRequest{Location: t.TempDir()}); !errors.Is(err, ErrSourcesDisabled) {
		t.Fatalf("AddSource error = %v, want ErrSourcesDisabled", err)
	}
	if _, err := catalog.RefreshSource(context.Background(), "x"); !errors.Is(err, ErrSourcesDisabled) {
		t.Fatalf("RefreshSource error = %v, want ErrSourcesDisabled", err)
	}
	if err := catalog.RemoveSource("x"); !errors.Is(err, ErrSourcesDisabled) {
		t.Fatalf("RemoveSource error = %v, want ErrSourcesDisabled", err)
	}
}

func TestLocalSourceRejectsBranch(t *testing.T) {
	catalog, _, _ := newSourceCatalog(t)
	sourceDir := filepath.Join(t.TempDir(), "source")
	writeManifest(t, sourceDir, sampleManifest("acme-one"))
	if _, err := catalog.AddSource(context.Background(), SourceRequest{
		Location: sourceDir,
		Branch:   "main",
	}); !errors.Is(err, ErrSourceInvalid) {
		t.Fatalf("AddSource error = %v, want ErrSourceInvalid", err)
	}
}

func TestRepositoryExampleManifestIsValid(t *testing.T) {
	dir := filepath.Join("..", "..", "..", "examples", "profile-source")
	manifest, err := readManifestFile(dir)
	if err != nil {
		t.Fatalf("readManifestFile: %v", err)
	}
	defs, meta, err := buildDefinitions(manifest, Source{
		ID:       "example",
		Kind:     SourceKindLocal,
		Location: dir,
	})
	if err != nil {
		t.Fatalf("buildDefinitions: %v", err)
	}
	if len(defs) != 1 || defs[0].Origin.Type != "third-party" || defs[0].Origin.Verified {
		t.Fatalf("unexpected definitions: %#v", defs)
	}
	if len(meta.ProfileIDs) != len(defs) {
		t.Fatalf("meta profile IDs = %v, want %d", meta.ProfileIDs, len(defs))
	}
}
