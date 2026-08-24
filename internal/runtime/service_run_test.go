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
