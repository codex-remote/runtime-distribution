package runtime

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

type managedProcess struct {
	service string
	command *exec.Cmd
	done    chan struct{}
	mu      sync.Mutex
	err     error
}

type managedExit struct {
	service string
	err     error
}

func runRuntimeSupervisor(ctx context.Context) error {
	paths, err := resolvePaths()
	if err != nil {
		return err
	}
	config, err := loadConfig(paths)
	if err != nil {
		return err
	}
	exits := make(chan managedExit, len(managedServices))
	processes := make([]*managedProcess, 0, len(managedServices))
	start := func(service string) (*managedProcess, error) {
		process, startErr := startManagedProcess(paths, service, exits)
		if startErr == nil {
			processes = append(processes, process)
		}
		return process, startErr
	}
	fail := func(startErr error) error {
		terminateManagedProcesses(processes)
		return startErr
	}

	postgres, err := start("postgres")
	if err != nil {
		return fail(err)
	}
	if err := waitManagedReady(ctx, postgres, func(readyCtx context.Context) error {
		return waitForPostgres(readyCtx, config.Toolchain, config.Ports.Postgres, 20*time.Second)
	}); err != nil {
		return fail(fmt.Errorf("PostgreSQL: %w", err))
	}

	valkey, err := start("valkey")
	if err != nil {
		return fail(err)
	}
	if err := waitManagedReady(ctx, valkey, func(readyCtx context.Context) error {
		return waitForPort(readyCtx, config.Ports.Valkey, 20*time.Second)
	}); err != nil {
		return fail(fmt.Errorf("Valkey: %w", err))
	}

	relay, err := start("relay")
	if err != nil {
		return fail(err)
	}
	if err := waitManagedReady(ctx, relay, func(readyCtx context.Context) error {
		return waitForHTTP(readyCtx, "http://127.0.0.1:"+strconv.Itoa(config.Ports.RunServer)+"/healthz", 30*time.Second)
	}); err != nil {
		return fail(fmt.Errorf("Run Server: %w", err))
	}

	if _, err := start("mac-agent"); err != nil {
		return fail(err)
	}
	gateway, err := start("gateway")
	if err != nil {
		return fail(err)
	}
	if err := waitManagedReady(ctx, gateway, func(readyCtx context.Context) error {
		return waitForHTTP(readyCtx, "http://127.0.0.1:"+strconv.Itoa(config.Ports.Gateway)+"/gateway/healthz", 20*time.Second)
	}); err != nil {
		return fail(fmt.Errorf("Gateway: %w", err))
	}

	select {
	case <-ctx.Done():
		terminateManagedProcesses(processes)
		return nil
	case exit := <-exits:
		terminateManagedProcesses(processes)
		if exit.err == nil {
			return fmt.Errorf("managed service %s exited unexpectedly", exit.service)
		}
		return fmt.Errorf("managed service %s exited: %w", exit.service, exit.err)
	}
}

func startManagedProcess(paths Paths, service string, exits chan<- managedExit) (*managedProcess, error) {
	executable, err := supervisorExecutable()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(paths.LogDir, 0o700); err != nil {
		return nil, err
	}
	stdout, err := os.OpenFile(filepath.Join(paths.LogDir, service+".stdout.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	stderr, err := os.OpenFile(filepath.Join(paths.LogDir, service+".stderr.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		_ = stdout.Close()
		return nil, err
	}
	command := exec.Command(executable, "service-run", service)
	command.Stdout = stdout
	command.Stderr = stderr
	command.Env = runtimeEnvironment(paths.StateDir)
	if err := command.Start(); err != nil {
		_ = stdout.Close()
		_ = stderr.Close()
		return nil, fmt.Errorf("start managed service %s: %w", service, err)
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

var supervisorExecutable = func() (string, error) {
	executable, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("resolve supervisor executable: %w", err)
	}
	executable, err = filepath.EvalSymlinks(executable)
	if err != nil {
		return "", fmt.Errorf("resolve supervisor symlink: %w", err)
	}
	return executable, nil
}

func runtimeEnvironment(stateDir string) []string {
	environment := make([]string, 0, len(os.Environ())+1)
	for _, value := range os.Environ() {
		if !strings.HasPrefix(value, "CODEX_REMOTE_HOME=") {
			environment = append(environment, value)
		}
	}
	return append(environment, "CODEX_REMOTE_HOME="+stateDir)
}

func waitManagedReady(ctx context.Context, process *managedProcess, ready func(context.Context) error) error {
	result := make(chan error, 1)
	go func() { result <- ready(ctx) }()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-process.done:
		return process.waitError()
	case err := <-result:
		return err
	}
}

func (process *managedProcess) waitError() error {
	<-process.done
	process.mu.Lock()
	defer process.mu.Unlock()
	return process.err
}

func terminateManagedProcesses(processes []*managedProcess) {
	for index := len(processes) - 1; index >= 0; index-- {
		process := processes[index]
		select {
		case <-process.done:
		default:
			if process.command.Process != nil {
				_ = process.command.Process.Signal(syscall.SIGTERM)
			}
		}
	}
	deadline := time.After(10 * time.Second)
	for _, process := range processes {
		select {
		case <-process.done:
		case <-deadline:
			for _, remaining := range processes {
				select {
				case <-remaining.done:
				default:
					if remaining.command.Process != nil {
						_ = remaining.command.Process.Kill()
					}
				}
			}
			return
		}
	}
}
