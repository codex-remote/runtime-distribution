package runtime

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type CodexCheck struct {
	Binary  string
	Version string
}

func discoverCodex(explicit string) (string, error) {
	candidates := make([]string, 0, 5)
	if strings.TrimSpace(explicit) != "" {
		candidates = append(candidates, explicit)
	} else if value := strings.TrimSpace(os.Getenv("CODEX_BINARY")); value != "" {
		candidates = append(candidates, value)
	} else {
		if value, err := exec.LookPath("codex"); err == nil {
			candidates = append(candidates, value)
		}
		candidates = append(candidates,
			"/Applications/ChatGPT.app/Contents/Resources/codex",
			"/Applications/Codex.app/Contents/Resources/codex",
		)
	}
	var failures []string
	for _, candidate := range candidates {
		resolved, err := resolveExecutable(candidate)
		if err == nil {
			return resolved, nil
		}
		failures = append(failures, fmt.Sprintf("%s (%v)", candidate, err))
	}
	if len(failures) == 0 {
		return "", errors.New("Codex CLI was not found; install Codex CLI or the Codex/ChatGPT app, then rerun setup")
	}
	return "", fmt.Errorf("Codex CLI was not usable: %s", strings.Join(failures, "; "))
}

func resolveExecutable(candidate string) (string, error) {
	value := strings.TrimSpace(candidate)
	if value == "" {
		return "", errors.New("path is empty")
	}
	path, err := exec.LookPath(value)
	if err != nil {
		return "", err
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", err
	}
	if info.IsDir() || info.Mode().Perm()&0o111 == 0 {
		return "", fmt.Errorf("%s is not executable", resolved)
	}
	return filepath.Clean(resolved), nil
}

func validateCodex(ctx context.Context, binary string) (CodexCheck, error) {
	if err := validateCodexBundle(binary); err != nil {
		return CodexCheck{}, err
	}
	versionOutput, err := commandOutput(ctx, binary, "--version")
	if err != nil {
		return CodexCheck{}, fmt.Errorf("run codex --version: %w", err)
	}
	if strings.TrimSpace(versionOutput) == "" {
		return CodexCheck{}, errors.New("codex --version returned no version")
	}
	if _, err := commandOutput(ctx, binary, "app-server", "--help"); err != nil {
		return CodexCheck{}, fmt.Errorf("Codex CLI does not provide app-server: %w", err)
	}
	if err := codexHandshake(ctx, binary); err != nil {
		return CodexCheck{}, err
	}
	return CodexCheck{Binary: binary, Version: strings.TrimSpace(versionOutput)}, nil
}

func validateCodexBundle(binary string) error {
	resources := filepath.Dir(binary)
	if filepath.Base(resources) != "Resources" || filepath.Base(filepath.Dir(resources)) != "Contents" {
		return nil
	}
	host := filepath.Join(resources, "codex-code-mode-host")
	if _, err := resolveExecutable(host); err != nil {
		return fmt.Errorf("Codex app installation is incomplete: missing executable %s; update or reinstall the app", host)
	}
	return nil
}

func commandOutput(ctx context.Context, binary string, arguments ...string) (string, error) {
	command := exec.CommandContext(ctx, binary, arguments...)
	output, err := command.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%w: %s", err, strings.TrimSpace(string(output)))
	}
	return string(output), nil
}

func codexHandshake(parent context.Context, binary string) error {
	ctx, cancel := context.WithTimeout(parent, 12*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, binary, "app-server")
	stdin, err := command.StdinPipe()
	if err != nil {
		return fmt.Errorf("open app-server stdin: %w", err)
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		return fmt.Errorf("open app-server stdout: %w", err)
	}
	stderr, err := command.StderrPipe()
	if err != nil {
		return fmt.Errorf("open app-server stderr: %w", err)
	}
	if err := command.Start(); err != nil {
		return fmt.Errorf("start Codex app-server: %w", err)
	}
	defer func() {
		_ = command.Process.Kill()
		_ = command.Wait()
	}()
	request := map[string]any{
		"method": "initialize",
		"id":     1,
		"params": map[string]any{"clientInfo": map[string]string{
			"name": "codex-remote-setup", "title": "Codex Remote Setup", "version": "1",
		}},
	}
	if err := json.NewEncoder(stdin).Encode(request); err != nil {
		return fmt.Errorf("send app-server initialize: %w", err)
	}
	type result struct {
		ID     int             `json:"id"`
		Result json.RawMessage `json:"result"`
		Error  json.RawMessage `json:"error"`
		Err    error
		Stderr string
	}
	response := make(chan result, 1)
	go func() {
		line, readErr := bufio.NewReader(stdout).ReadBytes('\n')
		var decoded result
		decoded.Err = readErr
		if readErr == nil {
			decoded.Err = json.Unmarshal(line, &decoded)
		}
		if decoded.Err != nil {
			stderrBytes, _ := io.ReadAll(io.LimitReader(stderr, 16*1024))
			decoded.Stderr = strings.TrimSpace(string(stderrBytes))
		}
		response <- decoded
	}()
	select {
	case <-ctx.Done():
		return errors.New("Codex app-server initialize handshake timed out")
	case decoded := <-response:
		if decoded.Err != nil {
			return fmt.Errorf("read Codex app-server initialize response: %v (%s)", decoded.Err, decoded.Stderr)
		}
		if decoded.ID != 1 || len(decoded.Error) != 0 || len(decoded.Result) == 0 {
			return fmt.Errorf("Codex app-server rejected initialize: error=%s", decoded.Error)
		}
	}
	if err := json.NewEncoder(stdin).Encode(map[string]any{"method": "initialized", "params": map[string]any{}}); err != nil {
		return fmt.Errorf("send app-server initialized: %w", err)
	}
	return nil
}
