package project

import (
	"strings"
	"testing"
)

func TestPluginTestCommands(t *testing.T) {
	migrate, rspec := pluginTestCommands([]string{"-S", "bundle"}, "my-plugin")
	wantMigrate := []string{"-S", "bundle", "exec", "rake", "db:create", "db:migrate"}
	wantRSpec := []string{"-S", "bundle", "exec", "rspec", "plugins/my-plugin/spec"}
	if strings.Join(migrate, " ") != strings.Join(wantMigrate, " ") {
		t.Fatalf("migrate args = %v, want %v", migrate, wantMigrate)
	}
	if strings.Join(rspec, " ") != strings.Join(wantRSpec, " ") {
		t.Fatalf("rspec args = %v, want %v", rspec, wantRSpec)
	}
}

func TestPluginTestEnvironmentIsolatesTestDatabase(t *testing.T) {
	values := make(map[string]string)
	for _, item := range pluginTestEnvironment("hxxddk_abc_demo_test") {
		values[item.Key] = item.Value
	}
	for key, want := range map[string]string{
		"RAILS_ENV":         "test",
		"DISCOURSE_ENV":     "test",
		"LOAD_PLUGINS":      "1",
		"RAILS_DB":          "hxxddk_abc_demo_test",
		"DISCOURSE_DB_NAME": "hxxddk_abc_demo_test",
	} {
		if values[key] != want {
			t.Fatalf("%s = %q, want %q", key, values[key], want)
		}
	}
}

func TestValidPluginSlug(t *testing.T) {
	for _, value := range []string{"my-plugin", "plugin_1", "a.b"} {
		if !validPluginSlug(value) {
			t.Errorf("validPluginSlug(%q) = false", value)
		}
	}
	for _, value := range []string{"", ".", "..", "../escape", "-plugin", "_plugin", "My-Plugin", "plugin/spec", strings.Repeat("a", 65)} {
		if validPluginSlug(value) {
			t.Errorf("validPluginSlug(%q) = true", value)
		}
	}
}
