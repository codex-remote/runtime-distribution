package runtime

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDiscoverAndValidateCodexThroughSymlink(t *testing.T) {
	directory := t.TempDir()
	target := filepath.Join(directory, "codex-real")
	script := `#!/bin/sh
case "$1" in
  --version) echo "codex-cli 9.9.9" ;;
  app-server)
    if [ "${2:-}" = "--help" ]; then echo "App Server"; exit 0; fi
    IFS= read -r request
    printf '%s\n' '{"id":1,"result":{"userAgent":"test"}}'
    IFS= read -r initialized
    ;;
  *) exit 2 ;;
esac
`
	if err := os.WriteFile(target, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	symlink := filepath.Join(directory, "codex")
	if err := os.Symlink(target, symlink); err != nil {
		t.Fatal(err)
	}
	resolved, err := discoverCodex(symlink)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := filepath.EvalSymlinks(target)
	if err != nil {
		t.Fatal(err)
	}
	if resolved != expected {
		t.Fatalf("resolved %q, want %q", resolved, expected)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	check, err := validateCodex(ctx, resolved)
	if err != nil {
		t.Fatal(err)
	}
	if check.Version != "codex-cli 9.9.9" {
		t.Fatalf("version = %q", check.Version)
	}
}

func TestDiscoverCodexReportsMissingExplicitBinary(t *testing.T) {
	_, err := discoverCodex(filepath.Join(t.TempDir(), "missing"))
	if err == nil {
		t.Fatal("expected missing explicit Codex binary to fail")
	}
}

func TestValidateCodexRejectsFailedHandshake(t *testing.T) {
	directory := t.TempDir()
	binary := filepath.Join(directory, "codex")
	script := `#!/bin/sh
case "$1" in
  --version) echo "codex-cli 9.9.9" ;;
  app-server)
    if [ "${2:-}" = "--help" ]; then echo "App Server"; exit 0; fi
    IFS= read -r request
    printf '%s\n' '{"id":1,"error":{"message":"not initialized"}}'
    ;;
esac
`
	if err := os.WriteFile(binary, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := validateCodex(ctx, binary); err == nil {
		t.Fatal("expected handshake failure")
	}
}
