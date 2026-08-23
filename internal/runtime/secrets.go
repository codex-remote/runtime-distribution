package runtime

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

const (
	postgresSecretService = "com.codex-remote.postgres-password"
	valkeySecretService   = "com.codex-remote.valkey-password"
)

func currentAccount() string {
	if value := strings.TrimSpace(os.Getenv("USER")); value != "" {
		return value
	}
	return fmt.Sprintf("uid-%d", os.Getuid())
}

func readSecret(ctx context.Context, service string) (string, error) {
	security, err := exec.LookPath("security")
	if err != nil {
		return "", errorsNew("macOS security command is unavailable")
	}
	command := exec.CommandContext(ctx, security, "find-generic-password", "-a", currentAccount(), "-s", service, "-w")
	output, err := command.Output()
	if err != nil {
		return "", fmt.Errorf("read %s from Keychain: %w", service, err)
	}
	value := strings.TrimSpace(string(output))
	if value == "" {
		return "", fmt.Errorf("Keychain item %s is empty", service)
	}
	return value, nil
}

func ensureSecret(ctx context.Context, service string) (string, bool, error) {
	if value, err := readSecret(ctx, service); err == nil {
		return value, false, nil
	}
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", false, err
	}
	value := hex.EncodeToString(bytes)
	security, err := exec.LookPath("security")
	if err != nil {
		return "", false, err
	}
	command := exec.CommandContext(ctx, security, "add-generic-password", "-U", "-a", currentAccount(), "-s", service, "-w", value)
	if output, err := command.CombinedOutput(); err != nil {
		return "", false, fmt.Errorf("save %s in Keychain: %w: %s", service, err, strings.TrimSpace(string(output)))
	}
	return value, true, nil
}

func deleteSecret(ctx context.Context, service string) {
	security, err := exec.LookPath("security")
	if err != nil {
		return
	}
	_ = exec.CommandContext(ctx, security, "delete-generic-password", "-a", currentAccount(), "-s", service).Run()
}
