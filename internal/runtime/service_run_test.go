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
	"sync"
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

func TestStopAllWaitsForTrackedProcessesAndPorts(t *testing.T) {
	originalLoaded := serviceLoaded
	originalBootout := bootoutService
	originalSnapshot := snapshotServiceProcessIDs
	originalProcessAlive := processIsAlive
	originalPortListening := runtimePortListening
	originalPollInterval := runtimeShutdownPollInterval
	t.Cleanup(func() {
		serviceLoaded = originalLoaded
		bootoutService = originalBootout
		snapshotServiceProcessIDs = originalSnapshot
		processIsAlive = originalProcessAlive
		runtimePortListening = originalPortListening
		runtimeShutdownPollInterval = originalPollInterval
	})

	var mu sync.Mutex
	loaded := true
	alive := map[int]bool{101: true, 102: true}
	listening := map[int]bool{54330: true}
	serviceLoaded = func(service string) bool {
		mu.Lock()
		defer mu.Unlock()
		return service == runtimeService && loaded
	}
	snapshotServiceProcessIDs = func(service string) []int {
		if service == runtimeService {
			return []int{101, 102}
		}
		return nil
	}
	processIsAlive = func(pid int) bool {
		mu.Lock()
		defer mu.Unlock()
		return alive[pid]
	}
	runtimePortListening = func(port int) bool {
		mu.Lock()
		defer mu.Unlock()
		return listening[port]
	}
	runtimeShutdownPollInterval = time.Millisecond
	bootoutService = func(_ context.Context, service string) error {
		if service != runtimeService {
			t.Fatalf("bootout service = %q, want %q", service, runtimeService)
		}
		mu.Lock()
		loaded = false
		mu.Unlock()
		go func() {
			time.Sleep(15 * time.Millisecond)
			mu.Lock()
			alive[101] = false
			alive[102] = false
			listening[54330] = false
			mu.Unlock()
		}()
		return nil
	}

	started := time.Now()
	config := Config{Ports: Ports{Postgres: 54330}}
	if err := stopAll(context.Background(), config); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(started); elapsed < 10*time.Millisecond {
		t.Fatalf("stopAll returned before tracked processes exited: %s", elapsed)
	}
}

func TestWaitForRuntimeShutdownReportsRemainingResources(t *testing.T) {
	originalLoaded := serviceLoaded
	originalProcessAlive := processIsAlive
	originalPortListening := runtimePortListening
	originalPollInterval := runtimeShutdownPollInterval
	t.Cleanup(func() {
		serviceLoaded = originalLoaded
		processIsAlive = originalProcessAlive
		runtimePortListening = originalPortListening
		runtimeShutdownPollInterval = originalPollInterval
	})
	serviceLoaded = func(service string) bool { return service == runtimeService }
	processIsAlive = func(pid int) bool { return pid == 42 }
	runtimePortListening = func(port int) bool { return port == 54330 }
	runtimeShutdownPollInterval = time.Millisecond

	err := waitForRuntimeShutdown(context.Background(), []int{42, 43}, []int{54330, 63800}, 5*time.Millisecond)
	if err == nil {
		t.Fatal("expected shutdown timeout")
	}
	for _, expected := range []string{serviceLabel(runtimeService), "process PIDs [42]", "listening ports [54330]"} {
		if !strings.Contains(err.Error(), expected) {
			t.Fatalf("shutdown error %q does not contain %q", err, expected)
		}
	}
	for _, absent := range []string{"process PIDs [42 43]", "listening ports [54330 63800]"} {
		if strings.Contains(err.Error(), absent) {
			t.Fatalf("shutdown error %q unexpectedly contains %q", err, absent)
		}
	}
}

func TestWaitForRuntimeShutdownHonorsCancellation(t *testing.T) {
	originalLoaded := serviceLoaded
	originalProcessAlive := processIsAlive
	originalPortListening := runtimePortListening
	originalPollInterval := runtimeShutdownPollInterval
	t.Cleanup(func() {
		serviceLoaded = originalLoaded
		processIsAlive = originalProcessAlive
		runtimePortListening = originalPortListening
		runtimeShutdownPollInterval = originalPollInterval
	})
	serviceLoaded = func(string) bool { return true }
	processIsAlive = func(int) bool { return false }
	runtimePortListening = func(int) bool { return false }
	runtimeShutdownPollInterval = time.Second

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := waitForRuntimeShutdown(ctx, nil, nil, time.Second); !errors.Is(err, context.Canceled) {
		t.Fatalf("shutdown error = %v, want context canceled", err)
	}
}

func TestParseLaunchctlPID(t *testing.T) {
	output := "gui/501/com.codex-remote.runtime = {\n\tstate = running\n\tpid = 753\n}"
	if got := parseLaunchctlPID(output); got != 753 {
		t.Fatalf("PID = %d, want 753", got)
	}
	if got := parseLaunchctlPID("state = waiting"); got != 0 {
		t.Fatalf("missing PID = %d, want 0", got)
	}
}

func TestDescendantProcessIDsIncludesEntireSnapshotTree(t *testing.T) {
	processes := "100 1\n101 100\n102 100\n103 101\n200 1\n"
	want := []int{100, 101, 102, 103}
	if got := descendantProcessIDs(100, processes); !reflect.DeepEqual(got, want) {
		t.Fatalf("process IDs = %v, want %v", got, want)
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
