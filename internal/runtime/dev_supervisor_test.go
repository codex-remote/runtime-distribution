package runtime

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateDevSupervisorOptionsRequiresIsolatedInputs(t *testing.T) {
	directory := t.TempDir()
	staticDir := filepath.Join(directory, "dist")
	if err := os.Mkdir(staticDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(staticDir, "index.html"), []byte("<html></html>"), 0o600); err != nil {
		t.Fatal(err)
	}
	paths := make(map[string]string)
	for _, name := range []string{"relay", "agent", "gateway", "codex"} {
		path := filepath.Join(directory, name)
		if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
			t.Fatal(err)
		}
		paths[name] = path
	}
	options := devSupervisorOptions{
		Relay: paths["relay"], Agent: paths["agent"], Gateway: paths["gateway"], CodexBinary: paths["codex"],
		StaticDir: staticDir, RelayAddr: "127.0.0.1:18875", AuthControl: "127.0.0.1:18876", GatewayAddr: "0.0.0.0:18874",
		LogDir: filepath.Join(directory, "logs"), WorkspaceRoots: []string{directory},
	}
	if err := validateDevSupervisorOptions(&options); err != nil {
		t.Fatal(err)
	}
	if len(options.WorkspaceRoots) != 1 {
		t.Fatalf("workspace roots = %#v", options.WorkspaceRoots)
	}
}

func TestRelayDevelopmentEnvironmentUsesRuntimeBoundaries(t *testing.T) {
	environment := relayDevelopmentEnvironment(devSupervisorOptions{
		RelayAddr: "127.0.0.1:18875", AuthControl: "127.0.0.1:18876",
		GatewayAddr: "0.0.0.0:18874", DatabaseURL: "postgres://dev", RedisURL: "redis://dev",
	})
	joined := strings.Join(environment, "\n")
	for _, expected := range []string{
		"RELAY_LISTEN_ADDR=127.0.0.1:18875",
		"AUTH_CONTROL_ADDR=127.0.0.1:18876",
		"RUNTIME_ALLOWED_ORIGIN=http://0.0.0.0:18874",
		"AUTH_ENABLED=true",
	} {
		if !strings.Contains(joined, expected) {
			t.Errorf("environment missing %q", expected)
		}
	}
}
