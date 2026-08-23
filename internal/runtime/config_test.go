package runtime

import (
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestConfigRoundTrip(t *testing.T) {
	directory := t.TempDir()
	paths := Paths{StateDir: directory, ConfigFile: filepath.Join(directory, "config.json")}
	want := Config{
		SchemaVersion:  configSchemaVersion,
		RuntimeVersion: "0.2.0",
		InstalledAt:    time.Date(2026, 8, 23, 10, 0, 0, 0, time.UTC),
		CodexBinary:    "/Applications/Codex.app/Contents/Resources/codex",
		CodexVersion:   "codex-cli 1.2.3",
		WorkspaceRoots: []string{"/work/a", "/work/b"},
		Ports:          defaultPorts,
		Toolchain:      Toolchain{InitDB: "/opt/initdb", ValkeyServer: "/opt/valkey-server"},
	}
	if err := saveConfig(paths, want); err != nil {
		t.Fatal(err)
	}
	got, err := loadConfig(paths)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("config round trip mismatch:\n got %#v\nwant %#v", got, want)
	}
}

func TestRenderedPlistContainsNoCredentialFields(t *testing.T) {
	contents := renderPlist("com.codex-remote.relay", "/opt/homebrew/bin/codex-remote", []string{"service-run", "relay"}, "/state", "/logs/out", "/logs/err")
	for _, forbidden := range []string{"RUNTIME_DATABASE_URL", "requirepass", "postgres-password", "valkey-password"} {
		if contains(contents, forbidden) {
			t.Fatalf("plist contains secret-related field %q", forbidden)
		}
	}
}

func contains(value, substring string) bool {
	for index := 0; index+len(substring) <= len(value); index++ {
		if value[index:index+len(substring)] == substring {
			return true
		}
	}
	return substring == ""
}
