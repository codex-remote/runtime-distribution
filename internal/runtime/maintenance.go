package runtime

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func ensureExternalMaintenance(config Config) error {
	switch strings.TrimSpace(os.Getenv("CODEX_REMOTE_EXECUTION_ORIGIN")) {
	case "external":
		return nil
	case "managed-agent":
		return errors.New("refusing to stop the Codex Remote stack from a Turn hosted by its Mac Agent; run this command in Terminal")
	case "", "auto":
	default:
		return errors.New("CODEX_REMOTE_EXECUTION_ORIGIN must be auto, external, or managed-agent")
	}
	pid := os.Getppid()
	needle := ":" + strconv.Itoa(config.Ports.RunServer) + "/ws/agent"
	for pid > 1 {
		commandOutput, _ := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "command=").Output()
		command := strings.TrimSpace(string(commandOutput))
		if strings.Contains(command, "mac-agent serve") && strings.Contains(command, needle) {
			return errors.New("refusing to stop the Codex Remote stack from a Turn hosted by its Mac Agent; run this command in Terminal")
		}
		parentOutput, err := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "ppid=").Output()
		if err != nil {
			break
		}
		parent, err := strconv.Atoi(strings.TrimSpace(string(parentOutput)))
		if err != nil || parent == pid {
			break
		}
		pid = parent
	}
	return nil
}

func backupDatabase(ctx context.Context, paths Paths, config Config, stdout io.Writer) (string, error) {
	if !portListening(config.Ports.Postgres) {
		return "", errors.New("PostgreSQL is not running; run codex-remote start first")
	}
	password, err := readSecret(ctx, postgresSecretService)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(paths.BackupDir, 0o700); err != nil {
		return "", err
	}
	path := filepath.Join(paths.BackupDir, "codexremote-"+time.Now().UTC().Format("20060102T150405Z")+".dump")
	command := exec.CommandContext(ctx, config.Toolchain.PGDump,
		"-h", "127.0.0.1", "-p", strconv.Itoa(config.Ports.Postgres),
		"-U", "codexremote", "-Fc", "-f", path, "codexremote",
	)
	command.Env = append(os.Environ(), "PGPASSWORD="+password)
	if output, err := command.CombinedOutput(); err != nil {
		_ = os.Remove(path)
		return "", fmt.Errorf("backup database: %w: %s", err, strings.TrimSpace(string(output)))
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return "", err
	}
	fmt.Fprintf(stdout, "Database backup: %s\n", path)
	return path, nil
}

func restoreDatabase(ctx context.Context, paths Paths, config Config, backup string, confirmed bool, stdout io.Writer) error {
	if !confirmed {
		return errors.New("rollback replaces the current database; rerun with --yes and --backup <file>")
	}
	if err := ensureExternalMaintenance(config); err != nil {
		return err
	}
	backup, err := filepath.Abs(backup)
	if err != nil {
		return err
	}
	if info, err := os.Stat(backup); err != nil || info.IsDir() {
		return fmt.Errorf("backup file is unavailable: %s", backup)
	}
	password, err := readSecret(ctx, postgresSecretService)
	if err != nil {
		return err
	}
	if err := stopAll(ctx); err != nil {
		return err
	}
	if err := bootstrapService(ctx, paths, "postgres"); err != nil {
		return err
	}
	if err := waitForPostgres(ctx, config.Toolchain, config.Ports.Postgres, 20*time.Second); err != nil {
		return err
	}
	environment := append(os.Environ(), "PGPASSWORD="+password)
	drop := exec.CommandContext(ctx, config.Toolchain.Dropdb, "--force", "-h", "127.0.0.1", "-p", strconv.Itoa(config.Ports.Postgres), "-U", "codexremote", "codexremote")
	drop.Env = environment
	if output, err := drop.CombinedOutput(); err != nil {
		return fmt.Errorf("drop database for rollback: %w: %s", err, strings.TrimSpace(string(output)))
	}
	create := exec.CommandContext(ctx, config.Toolchain.Createdb, "-h", "127.0.0.1", "-p", strconv.Itoa(config.Ports.Postgres), "-U", "codexremote", "codexremote")
	create.Env = environment
	if output, err := create.CombinedOutput(); err != nil {
		return fmt.Errorf("create database for rollback: %w: %s", err, strings.TrimSpace(string(output)))
	}
	restore := exec.CommandContext(ctx, config.Toolchain.PGRestore,
		"-h", "127.0.0.1", "-p", strconv.Itoa(config.Ports.Postgres), "-U", "codexremote", "-d", "codexremote", "--clean", "--if-exists", backup,
	)
	restore.Env = environment
	if output, err := restore.CombinedOutput(); err != nil {
		return fmt.Errorf("restore database: %w: %s", err, strings.TrimSpace(string(output)))
	}
	if err := startAll(ctx, paths, config); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "Database restored from %s\n", backup)
	return nil
}

func bootstrapService(ctx context.Context, paths Paths, service string) error {
	if serviceLoaded(service) {
		return nil
	}
	command := exec.CommandContext(ctx, "launchctl", "bootstrap", launchDomain(), plistPath(paths, service))
	if output, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("start %s: %w: %s", service, err, strings.TrimSpace(string(output)))
	}
	return nil
}

func migrate(ctx context.Context, paths Paths, config Config, stdout io.Writer) error {
	if err := ensureExternalMaintenance(config); err != nil {
		return err
	}
	if _, err := backupDatabase(ctx, paths, config, stdout); err != nil {
		return err
	}
	for _, service := range []string{"mac-agent", "relay"} {
		if serviceLoaded(service) {
			command := exec.CommandContext(ctx, "launchctl", "bootout", launchDomain()+"/"+serviceLabel(service))
			if output, err := command.CombinedOutput(); err != nil {
				return fmt.Errorf("stop %s: %w: %s", service, err, strings.TrimSpace(string(output)))
			}
		}
	}
	if err := bootstrapService(ctx, paths, "relay"); err != nil {
		return err
	}
	if err := waitForHTTP(ctx, "http://127.0.0.1:"+strconv.Itoa(config.Ports.RunServer)+"/healthz", 30*time.Second); err != nil {
		return err
	}
	if err := bootstrapService(ctx, paths, "mac-agent"); err != nil {
		return err
	}
	fmt.Fprintln(stdout, "Embedded database migrations completed and services are healthy.")
	return nil
}

func uninstall(ctx context.Context, paths Paths, config Config, purge, confirmed bool, stdout io.Writer) error {
	if err := ensureExternalMaintenance(config); err != nil {
		return err
	}
	if purge && !confirmed {
		return errors.New("--purge permanently deletes Codex Remote data; rerun with --purge --yes")
	}
	if err := stopAll(ctx); err != nil {
		return err
	}
	for _, service := range services {
		if err := os.Remove(plistPath(paths, service)); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	if !purge {
		fmt.Fprintf(stdout, "Services removed. Data retained at %s\n", paths.StateDir)
		fmt.Fprintln(stdout, "Homebrew files can now be removed with: brew uninstall codex-remote")
		return nil
	}
	if paths.StateDir == "/" || paths.StateDir == "." || filepath.Dir(paths.StateDir) == paths.StateDir {
		return fmt.Errorf("refusing to purge unsafe state path %s", paths.StateDir)
	}
	deleteSecret(ctx, postgresSecretService)
	deleteSecret(ctx, valkeySecretService)
	if err := os.RemoveAll(paths.StateDir); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "Purged Codex Remote state at %s\n", paths.StateDir)
	return nil
}
