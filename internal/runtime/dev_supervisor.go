package runtime

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type devSupervisorOptions struct {
	Relay          string
	Agent          string
	Gateway        string
	StaticDir      string
	CodexBinary    string
	WorkspaceRoots []string
	RelayAddr      string
	AuthControl    string
	GatewayAddr    string
	DatabaseURL    string
	RedisURL       string
	LogDir         string
}

func runDevSupervisor(ctx context.Context, options devSupervisorOptions, stdout io.Writer) error {
	if err := validateDevSupervisorOptions(&options); err != nil {
		return err
	}
	if err := os.MkdirAll(options.LogDir, 0o700); err != nil {
		return fmt.Errorf("create development supervisor log directory: %w", err)
	}
	if _, err := os.Stat(filepath.Join(options.StaticDir, "index.html")); err != nil {
		return fmt.Errorf("Mobile Web build is missing at %s: %w", options.StaticDir, err)
	}

	exits := make(chan managedExit, 3)
	processes := make([]*managedProcess, 0, 3)
	start := func(service, executable string, arguments []string, environment []string) (*managedProcess, error) {
		process, err := startDevManagedProcess(service, executable, arguments, environment, options.LogDir, exits)
		if err == nil {
			processes = append(processes, process)
		}
		return process, err
	}
	fail := func(err error) error {
		terminateManagedProcesses(processes)
		return err
	}

	if _, err := start("relay", options.Relay, []string{"--listen", options.RelayAddr}, relayDevelopmentEnvironment(options)); err != nil {
		return fail(err)
	}
	if err := waitForHTTP(ctx, "http://"+options.RelayAddr+"/healthz", 30*time.Second); err != nil {
		return fail(fmt.Errorf("Relay: %w", err))
	}

	agentArguments := []string{"serve", "--relay-url", "ws://" + options.RelayAddr + "/ws/agent", "--codex-binary", options.CodexBinary, "--name", "leehoo-mac-mobileweb-supervised"}
	if _, err := start("mac-agent", options.Agent, agentArguments, agentDevelopmentEnvironment(options)); err != nil {
		return fail(err)
	}
	if err := waitForAgentConnected(ctx, "http://"+options.RelayAddr+"/status", 30*time.Second); err != nil {
		return fail(fmt.Errorf("Mac Agent: %w", err))
	}

	gatewayArguments := []string{"--listen", options.GatewayAddr, "--upstream", "http://" + options.RelayAddr, "--static", options.StaticDir}
	if _, err := start("gateway", options.Gateway, gatewayArguments, os.Environ()); err != nil {
		return fail(err)
	}
	if err := waitForHTTP(ctx, "http://"+options.GatewayAddr+"/gateway/healthz", 20*time.Second); err != nil {
		return fail(fmt.Errorf("Gateway: %w", err))
	}

	fmt.Fprintf(stdout, "Development Runtime Supervisor ready: gateway=http://%s relay=http://%s\n", options.GatewayAddr, options.RelayAddr)
	select {
	case <-ctx.Done():
		terminateManagedProcesses(processes)
		return nil
	case exit := <-exits:
		terminateManagedProcesses(processes)
		if exit.err == nil {
			return fmt.Errorf("managed development service %s exited unexpectedly", exit.service)
		}
		return fmt.Errorf("managed development service %s exited: %w", exit.service, exit.err)
	}
}

func validateDevSupervisorOptions(options *devSupervisorOptions) error {
	for label, value := range map[string]string{
		"Relay": options.Relay, "Mac Agent": options.Agent, "Gateway": options.Gateway,
		"Codex": options.CodexBinary, "Static directory": options.StaticDir,
		"Relay address": options.RelayAddr, "Auth Control address": options.AuthControl,
		"Gateway address": options.GatewayAddr, "Log directory": options.LogDir,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("development supervisor requires %s", label)
		}
	}
	for label, value := range map[string]string{"Relay": options.Relay, "Mac Agent": options.Agent, "Gateway": options.Gateway, "Codex": options.CodexBinary} {
		if _, err := resolveExecutable(value); err != nil {
			return fmt.Errorf("%s executable %s: %w", label, value, err)
		}
	}
	roots, err := normalizeWorkspaceRoots(options.WorkspaceRoots)
	if err != nil {
		return err
	}
	options.WorkspaceRoots = roots
	return nil
}

func startDevManagedProcess(service, executable string, arguments []string, environment []string, logDir string, exits chan<- managedExit) (*managedProcess, error) {
	stdout, err := os.OpenFile(filepath.Join(logDir, service+".stdout.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	stderr, err := os.OpenFile(filepath.Join(logDir, service+".stderr.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		_ = stdout.Close()
		return nil, err
	}
	command := exec.Command(executable, arguments...)
	command.Stdout = stdout
	command.Stderr = stderr
	command.Env = environment
	if err := command.Start(); err != nil {
		_ = stdout.Close()
		_ = stderr.Close()
		return nil, fmt.Errorf("start development service %s: %w", service, err)
	}
	_ = stdout.Close()
	_ = stderr.Close()
	process := &managedProcess{service: service, command: command, done: make(chan struct{})}
	go func() {
		runErr := command.Wait()
		process.mu.Lock()
		process.err = runErr
		process.mu.Unlock()
		close(process.done)
		if exits != nil {
			exits <- managedExit{service: service, err: runErr}
		}
	}()
	return process, nil
}

func relayDevelopmentEnvironment(options devSupervisorOptions) []string {
	environment := append([]string{}, os.Environ()...)
	environment = append(environment,
		"RELAY_LISTEN_ADDR="+options.RelayAddr,
		"AUTH_CONTROL_ADDR="+options.AuthControl,
		"RUNTIME_DATABASE_URL="+options.DatabaseURL,
		"RUNTIME_REDIS_URL="+options.RedisURL,
		"RUNTIME_ALLOWED_ORIGIN=http://"+options.GatewayAddr,
		"AUTH_ENABLED=true",
		"AUTH_COOKIE_SECURE=false",
	)
	return environment
}

func agentDevelopmentEnvironment(options devSupervisorOptions) []string {
	environment := append([]string{}, os.Environ()...)
	roots := strings.Join(options.WorkspaceRoots, ",")
	return append(environment, "AGENT_WORKSPACE_ROOTS="+roots)
}

func waitForAgentConnected(ctx context.Context, endpoint string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		requestContext, cancel := context.WithTimeout(ctx, 2*time.Second)
		request, err := http.NewRequestWithContext(requestContext, http.MethodGet, endpoint, nil)
		if err == nil {
			response, requestErr := http.DefaultClient.Do(request)
			if requestErr == nil {
				body, readErr := io.ReadAll(io.LimitReader(response.Body, 64*1024))
				_ = response.Body.Close()
				if readErr == nil && response.StatusCode == http.StatusOK && strings.Contains(string(body), `"agent_connected":true`) {
					cancel()
					return nil
				}
			}
		}
		cancel()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
	return errors.New("agent did not become connected before timeout")
}
