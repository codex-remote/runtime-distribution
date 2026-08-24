package runtime

import (
	"os"
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
		Toolchain:      Toolchain{InitDB: "/opt/initdb"},
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
	contents := renderPlist("com.codex-remote.runtime", "/opt/homebrew/bin/codex-remote", []string{"service-run", "runtime"}, "/state", "/logs/out", "/logs/err", map[string]string{"CODEX_REMOTE_HOME": "/state"})
	for _, forbidden := range []string{"RUNTIME_DATABASE_URL", "requirepass", "postgres-password", "valkey-password"} {
		if contains(contents, forbidden) {
			t.Fatalf("plist contains secret-related field %q", forbidden)
		}
	}
	if !contains(contents, "CODEX_REMOTE_HOME") || !contains(contents, "/state") {
		t.Fatal("plist does not persist the runtime state directory")
	}
}

func TestWriteLaunchAgentsCreatesOneRuntimePlist(t *testing.T) {
	directory := t.TempDir()
	paths := Paths{LaunchAgents: directory, StateDir: "/state", LogDir: "/logs"}
	if err := writeLaunchAgents(paths, Layout{CLI: "/opt/homebrew/bin/codex-remote"}); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "com.codex-remote.runtime.plist" {
		t.Fatalf("LaunchAgent files = %v, want only com.codex-remote.runtime.plist", entries)
	}
	contents, err := os.ReadFile(filepath.Join(directory, entries[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	if !contains(string(contents), "<string>service-run</string>") || !contains(string(contents), "<string>runtime</string>") {
		t.Fatalf("runtime plist has unexpected arguments:\n%s", contents)
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
