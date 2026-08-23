package runtime

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

func runService(service string) error {
	paths, err := resolvePaths()
	if err != nil {
		return err
	}
	config, err := loadConfig(paths)
	if err != nil {
		return err
	}
	layout, err := resolveLayout()
	if err != nil {
		return err
	}
	switch service {
	case "postgres":
		return execReplace(config.Toolchain.Postgres, []string{
			"postgres", "-D", paths.PostgresData, "-h", "127.0.0.1", "-p", strconv.Itoa(config.Ports.Postgres),
		}, os.Environ())
	case "valkey":
		ctx := context.Background()
		password, err := readSecret(ctx, valkeySecretService)
		if err != nil {
			return err
		}
		configuration := strings.Join([]string{
			"bind 127.0.0.1",
			"protected-mode yes",
			"port " + strconv.Itoa(config.Ports.Valkey),
			"dir " + paths.DataDir,
			"dbfilename valkey.rdb",
			"appendonly yes",
			"appendfilename valkey.aof",
			"requirepass " + password,
			"",
		}, "\n")
		return runChildWithInput(layout.ValkeyServer, []string{"-"}, configuration)
	case "relay":
		postgresPassword, err := readSecret(context.Background(), postgresSecretService)
		if err != nil {
			return err
		}
		valkeyPassword, err := readSecret(context.Background(), valkeySecretService)
		if err != nil {
			return err
		}
		postgresURL := (&url.URL{
			Scheme: "postgres", User: url.UserPassword("codexremote", postgresPassword),
			Host: "127.0.0.1:" + strconv.Itoa(config.Ports.Postgres), Path: "/codexremote",
			RawQuery: "sslmode=disable",
		}).String()
		valkeyURL := (&url.URL{
			Scheme: "redis", User: url.UserPassword("default", valkeyPassword),
			Host: "127.0.0.1:" + strconv.Itoa(config.Ports.Valkey), Path: "/0",
		}).String()
		environment := append(os.Environ(),
			"RELAY_LISTEN_ADDR=127.0.0.1:"+strconv.Itoa(config.Ports.RunServer),
			"AUTH_CONTROL_ADDR=127.0.0.1:"+strconv.Itoa(config.Ports.AuthControl),
			"RUNTIME_DATABASE_URL="+postgresURL,
			"RUNTIME_REDIS_URL="+valkeyURL,
			"RUNTIME_ALLOWED_ORIGIN=http://127.0.0.1:"+strconv.Itoa(config.Ports.Gateway),
			"AUTH_ENABLED=true",
			"AUTH_COOKIE_SECURE=false",
		)
		return execReplace(layout.Relay, []string{"relay-server"}, environment)
	case "mac-agent":
		arguments := []string{
			"mac-agent", "serve",
			"--relay-url", "ws://127.0.0.1:" + strconv.Itoa(config.Ports.RunServer) + "/ws/agent",
			"--codex-binary", config.CodexBinary,
			"--runtime-db", filepath.Join(paths.DataDir, "agent-runtime.sqlite3"),
		}
		if hostname, hostErr := os.Hostname(); hostErr == nil && hostname != "" {
			arguments = append(arguments, "--name", hostname)
		}
		for _, root := range config.WorkspaceRoots {
			arguments = append(arguments, "--workspace-root", root)
		}
		return execReplace(layout.MacAgent, arguments, os.Environ())
	case "gateway":
		return execReplace(layout.Gateway, []string{
			"mobile-web-gateway",
			"--listen", "0.0.0.0:" + strconv.Itoa(config.Ports.Gateway),
			"--upstream", "http://127.0.0.1:" + strconv.Itoa(config.Ports.RunServer),
			"--static", layout.MobileWeb,
		}, os.Environ())
	default:
		return fmt.Errorf("unknown internal service %q", service)
	}
}

func execReplace(binary string, arguments, environment []string) error {
	return syscall.Exec(binary, arguments, environment)
}

func runChildWithInput(binary string, arguments []string, input string) error {
	command := exec.Command(binary, arguments...)
	command.Stdin = strings.NewReader(input)
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	if err := command.Start(); err != nil {
		return err
	}
	signals := make(chan os.Signal, 2)
	signal.Notify(signals, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	for {
		select {
		case received := <-signals:
			if command.Process != nil {
				_ = command.Process.Signal(received)
			}
		case err := <-done:
			signal.Stop(signals)
			return err
		}
	}
}
