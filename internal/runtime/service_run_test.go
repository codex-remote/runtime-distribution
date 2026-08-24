package runtime

import (
	"bytes"
	"context"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestPostgresArgvZeroUsesResolvedExecutable(t *testing.T) {
	config := Config{Toolchain: Toolchain{Postgres: "/opt/homebrew/opt/postgresql@17/bin/postgres"}, Ports: Ports{Postgres: 54330}}
	arguments := postgresArguments(config, Paths{PostgresData: "/state/postgres"})
	if arguments[0] != config.Toolchain.Postgres {
		t.Fatalf("argv[0] = %q, want resolved executable %q", arguments[0], config.Toolchain.Postgres)
	}
}

func TestValkeyConfigurationQuotesPathAndPassword(t *testing.T) {
	configuration, err := valkeyConfiguration(
		Config{Ports: Ports{Valkey: 63800}},
		Paths{DataDir: `/Users/test/Library/Application Support/CodexRemote/Data "beta"\state`},
		`secret "value"\suffix`,
	)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		`dir "/Users/test/Library/Application Support/CodexRemote/Data \"beta\"\\state"`,
		`requirepass "secret \"value\"\\suffix"`,
	} {
		if !strings.Contains(configuration, expected) {
			t.Fatalf("configuration does not contain %q:\n%s", expected, configuration)
		}
	}
}

func TestValkeyConfigurationRejectsLineBreaks(t *testing.T) {
	_, err := valkeyConfiguration(Config{}, Paths{DataDir: "/state\ninclude /tmp/other.conf"}, "secret")
	if err == nil {
		t.Fatal("expected a forbidden control-character error")
	}
}

func TestBundledValkeyAcceptsQuotedSpacePath(t *testing.T) {
	binary := os.Getenv("CODEX_REMOTE_VALKEY_TEST_BINARY")
	if binary == "" {
		t.Skip("CODEX_REMOTE_VALKEY_TEST_BINARY is not set")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	dataDir := filepath.Join(t.TempDir(), "Application Support", "CodexRemote", "Data")
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		t.Fatal(err)
	}
	configuration, err := valkeyConfiguration(Config{Ports: Ports{Valkey: port}}, Paths{DataDir: dataDir}, `test "password"\value`)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	command := exec.Command(binary, "-")
	command.Stdin = strings.NewReader(configuration)
	command.Stdout = &output
	command.Stderr = &output
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	t.Cleanup(func() {
		if command.Process != nil {
			_ = command.Process.Signal(syscall.SIGTERM)
		}
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			if command.Process != nil {
				_ = command.Process.Kill()
			}
		}
	})
	address := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		connection, dialErr := net.DialTimeout("tcp", address, 100*time.Millisecond)
		if dialErr == nil {
			_ = connection.Close()
			return
		}
		select {
		case runErr := <-done:
			t.Fatalf("Valkey exited before listening: %v\n%s", runErr, output.String())
		default:
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("Valkey did not listen on %s:\n%s", address, output.String())
}

func TestStopServicesRollsBackInReverseOrder(t *testing.T) {
	original := bootoutService
	t.Cleanup(func() { bootoutService = original })
	var stopped []string
	bootoutService = func(_ context.Context, service string) error {
		stopped = append(stopped, service)
		if service == "valkey" {
			return errors.New("test failure")
		}
		return nil
	}
	err := stopServices(context.Background(), []string{"postgres", "valkey", "relay"})
	if err == nil || !strings.Contains(err.Error(), "valkey") {
		t.Fatalf("stopServices error = %v, want valkey failure", err)
	}
	if want := []string{"relay", "valkey", "postgres"}; !reflect.DeepEqual(stopped, want) {
		t.Fatalf("stop order = %v, want %v", stopped, want)
	}
}

func TestRemoveLegacyLaunchAgentsStopsAndDeletesOnlyLegacyJobs(t *testing.T) {
	originalLoaded := serviceLoaded
	originalBootout := bootoutService
	t.Cleanup(func() {
		serviceLoaded = originalLoaded
		bootoutService = originalBootout
	})
	serviceLoaded = func(service string) bool { return service == "postgres" || service == "valkey" }
	var stopped []string
	bootoutService = func(_ context.Context, service string) error {
		stopped = append(stopped, service)
		return nil
	}
	directory := t.TempDir()
	paths := Paths{LaunchAgents: directory}
	for _, service := range allLaunchAgentServices() {
		if err := os.WriteFile(plistPath(paths, service), []byte(service), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := removeLegacyLaunchAgents(context.Background(), paths); err != nil {
		t.Fatal(err)
	}
	if want := []string{"valkey", "postgres"}; !reflect.DeepEqual(stopped, want) {
		t.Fatalf("stopped = %v, want %v", stopped, want)
	}
	if _, err := os.Stat(plistPath(paths, runtimeService)); err != nil {
		t.Fatalf("runtime plist was removed: %v", err)
	}
	for _, service := range managedServices {
		if _, err := os.Stat(plistPath(paths, service)); !os.IsNotExist(err) {
			t.Fatalf("legacy plist %s still exists", service)
		}
	}
}

func TestRuntimeEnvironmentReplacesStateDirectory(t *testing.T) {
	t.Setenv("CODEX_REMOTE_HOME", "/old")
	values := runtimeEnvironment("/new state")
	var found []string
	for _, value := range values {
		if strings.HasPrefix(value, "CODEX_REMOTE_HOME=") {
			found = append(found, value)
		}
	}
	if want := []string{"CODEX_REMOTE_HOME=/new state"}; !reflect.DeepEqual(found, want) {
		t.Fatalf("CODEX_REMOTE_HOME entries = %v, want %v", found, want)
	}
}

func TestManagedProcessUsesSupervisorCommandEnvironmentAndLogs(t *testing.T) {
	originalExecutable := supervisorExecutable
	t.Cleanup(func() { supervisorExecutable = originalExecutable })
	directory := t.TempDir()
	helper := filepath.Join(directory, "codex-remote-helper")
	script := "#!/bin/sh\ntrap 'exit 0' TERM INT\necho \"$CODEX_REMOTE_HOME|$1|$2\"\nwhile :; do sleep 1; done\n"
	if err := os.WriteFile(helper, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	supervisorExecutable = func() (string, error) { return helper, nil }
	paths := Paths{StateDir: "/state with spaces", LogDir: filepath.Join(directory, "logs")}
	process, err := startManagedProcess(paths, "relay", make(chan managedExit, 1))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { terminateManagedProcesses([]*managedProcess{process}) })
	logPath := filepath.Join(paths.LogDir, "relay.stdout.log")
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		contents, readErr := os.ReadFile(logPath)
		if readErr == nil && strings.Contains(string(contents), "/state with spaces|service-run|relay") {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	contents, _ := os.ReadFile(logPath)
	t.Fatalf("managed process log = %q", contents)
}

func TestPostgresEnvironmentDefinesLaunchdSafeLocale(t *testing.T) {
	t.Setenv("LC_ALL", "zh_CN.UTF-8")
	count := 0
	for _, value := range postgresEnvironment() {
		if strings.HasPrefix(value, "LC_ALL=") {
			count++
			if value != "LC_ALL=C" {
				t.Fatalf("LC_ALL = %q, want C", value)
			}
		}
	}
	if count != 1 {
		t.Fatalf("LC_ALL entries = %d, want 1", count)
	}
}
