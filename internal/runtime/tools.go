package runtime

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

var defaultPorts = Ports{Gateway: 18774, RunServer: 18775, AuthControl: 18776, Postgres: 54329, Valkey: 63799}

func discoverToolchain(ctx context.Context) (Toolchain, error) {
	postgresBin, err := formulaBin(ctx, "postgresql@17", "initdb")
	if err != nil {
		return Toolchain{}, fmt.Errorf("PostgreSQL 17 is required: %w", err)
	}
	valkeyBin, err := formulaBin(ctx, "valkey", "valkey-server")
	if err != nil {
		return Toolchain{}, fmt.Errorf("Valkey is required: %w", err)
	}
	toolchain := Toolchain{
		InitDB:       filepath.Join(postgresBin, "initdb"),
		Postgres:     filepath.Join(postgresBin, "postgres"),
		PGIsReady:    filepath.Join(postgresBin, "pg_isready"),
		Createdb:     filepath.Join(postgresBin, "createdb"),
		Dropdb:       filepath.Join(postgresBin, "dropdb"),
		PGDump:       filepath.Join(postgresBin, "pg_dump"),
		PGRestore:    filepath.Join(postgresBin, "pg_restore"),
		PSQL:         filepath.Join(postgresBin, "psql"),
		ValkeyServer: filepath.Join(valkeyBin, "valkey-server"),
		ValkeyCLI:    filepath.Join(valkeyBin, "valkey-cli"),
	}
	for _, path := range []string{
		toolchain.InitDB, toolchain.Postgres, toolchain.PGIsReady, toolchain.Createdb,
		toolchain.Dropdb, toolchain.PGDump, toolchain.PGRestore, toolchain.PSQL, toolchain.ValkeyServer, toolchain.ValkeyCLI,
	} {
		if _, err := resolveExecutable(path); err != nil {
			return Toolchain{}, fmt.Errorf("required dependency executable is unavailable at %s: %w", path, err)
		}
	}
	return toolchain, nil
}

func formulaBin(ctx context.Context, formula, executable string) (string, error) {
	if brew, err := exec.LookPath("brew"); err == nil {
		output, runErr := commandOutput(ctx, brew, "--prefix", formula)
		if runErr == nil {
			return filepath.Join(strings.TrimSpace(output), "bin"), nil
		}
	}
	path, err := exec.LookPath(executable)
	if err != nil {
		return "", fmt.Errorf("brew --prefix %s failed and %s was not in PATH", formula, executable)
	}
	return filepath.Dir(path), nil
}

func selectPorts() (Ports, error) {
	ports := defaultPorts
	used := make(map[int]bool)
	values := []*int{&ports.Gateway, &ports.RunServer, &ports.AuthControl, &ports.Postgres, &ports.Valkey}
	for _, value := range values {
		port, err := findAvailablePort(*value, used)
		if err != nil {
			return Ports{}, err
		}
		*value = port
		used[port] = true
	}
	return ports, nil
}

func findAvailablePort(preferred int, used map[int]bool) (int, error) {
	for offset := 0; offset < 1000; offset++ {
		candidate := preferred + offset
		if candidate > 65535 || used[candidate] {
			continue
		}
		listener, err := net.Listen("tcp4", "0.0.0.0:"+strconv.Itoa(candidate))
		if err != nil {
			continue
		}
		_ = listener.Close()
		return candidate, nil
	}
	return 0, fmt.Errorf("no available port found near %d", preferred)
}

func portListening(port int) bool {
	connection, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), 300*time.Millisecond)
	if err != nil {
		return false
	}
	_ = connection.Close()
	return true
}

func listenerDescription(port int) string {
	lsof, err := exec.LookPath("lsof")
	if err != nil {
		return "unknown listener (lsof unavailable)"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, lsof, "-nP", "-iTCP:"+strconv.Itoa(port), "-sTCP:LISTEN")
	output, err := command.CombinedOutput()
	if err != nil || strings.TrimSpace(string(output)) == "" {
		return "unknown listener"
	}
	return strings.TrimSpace(string(output))
}

func normalizeWorkspaceRoots(values []string) ([]string, error) {
	if len(values) == 0 {
		cwd, err := os.Getwd()
		if err != nil {
			return nil, err
		}
		values = []string{cwd}
	}
	result := make([]string, 0, len(values))
	seen := make(map[string]bool)
	for _, value := range values {
		absolute, err := filepath.Abs(strings.TrimSpace(value))
		if err != nil {
			return nil, fmt.Errorf("resolve workspace root %q: %w", value, err)
		}
		resolved, err := filepath.EvalSymlinks(absolute)
		if err != nil {
			return nil, fmt.Errorf("workspace root %s: %w", absolute, err)
		}
		info, err := os.Stat(resolved)
		if err != nil || !info.IsDir() {
			return nil, fmt.Errorf("workspace root is not a directory: %s", resolved)
		}
		if !seen[resolved] {
			seen[resolved] = true
			result = append(result, resolved)
		}
	}
	return result, nil
}

func acquireLock(paths Paths) (func(), error) {
	if err := os.MkdirAll(paths.StateDir, 0o700); err != nil {
		return nil, err
	}
	if err := os.Mkdir(paths.LockDir, 0o700); err != nil {
		if os.IsExist(err) {
			return nil, errorsNew("another Codex Remote setup, upgrade, or maintenance operation is running")
		}
		return nil, err
	}
	return func() { _ = os.Remove(paths.LockDir) }, nil
}

func errorsNew(value string) error { return fmt.Errorf("%s", value) }
