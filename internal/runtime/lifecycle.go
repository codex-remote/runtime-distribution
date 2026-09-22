package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type setupOptions struct {
	CodexBinary    string
	WorkspaceRoots []string
	Repair         bool
	Start          bool
}

func setup(ctx context.Context, version string, options setupOptions, stdout io.Writer) error {
	if goruntime.GOOS != "darwin" || goruntime.GOARCH != "arm64" {
		return errors.New("Codex Remote currently supports only macOS on Apple Silicon (darwin-arm64)")
	}
	paths, err := resolvePaths()
	if err != nil {
		return err
	}
	if _, statErr := os.Stat(paths.ConfigFile); statErr == nil && !options.Repair {
		return errors.New("Codex Remote is already set up; use setup --repair to validate and repair it")
	}
	layout, err := resolveLayout()
	if err != nil {
		return err
	}
	if err := layout.validate(); err != nil {
		return err
	}

	var existing Config
	hasExisting := false
	if options.Repair {
		if loaded, loadErr := loadConfig(paths); loadErr == nil {
			existing = loaded
			hasExisting = true
		}
	}
	explicitCodex := options.CodexBinary
	if explicitCodex == "" && hasExisting {
		if _, resolveErr := resolveExecutable(existing.CodexBinary); resolveErr == nil {
			explicitCodex = existing.CodexBinary
		}
	}
	codexBinary, err := discoverCodex(explicitCodex)
	if err != nil {
		return err
	}
	checkContext, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	codex, err := validateCodex(checkContext, codexBinary)
	if err != nil {
		return err
	}
	toolchain, err := discoverToolchain(checkContext)
	if err != nil {
		return err
	}
	workspaceValues := options.WorkspaceRoots
	if len(workspaceValues) == 0 && hasExisting {
		workspaceValues = existing.WorkspaceRoots
	}
	workspaceRoots, err := normalizeWorkspaceRoots(workspaceValues)
	if err != nil {
		return err
	}
	ports := defaultPorts
	if hasExisting {
		ports = existing.Ports
	} else if ports, err = selectPorts(); err != nil {
		return err
	}

	unlock, err := acquireLock(paths)
	if err != nil {
		return err
	}
	defer unlock()
	for _, directory := range []string{paths.DataDir, paths.RunDir, paths.LogDir, paths.BackupDir} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			return err
		}
	}
	postgresPassword, postgresCreated, err := ensureSecret(ctx, postgresSecretService)
	if err != nil {
		return err
	}
	_, valkeyCreated, err := ensureSecret(ctx, valkeySecretService)
	if err != nil {
		if postgresCreated {
			deleteSecret(ctx, postgresSecretService)
		}
		return err
	}
	if err := initializePostgres(ctx, paths, toolchain, ports.Postgres, postgresPassword); err != nil {
		if postgresCreated {
			deleteSecret(ctx, postgresSecretService)
		}
		if valkeyCreated {
			deleteSecret(ctx, valkeySecretService)
		}
		return err
	}
	installedAt := time.Now().UTC()
	if hasExisting {
		installedAt = existing.InstalledAt
	}
	config := Config{
		SchemaVersion: configSchemaVersion, RuntimeVersion: version, InstalledAt: installedAt,
		CodexBinary: codex.Binary, CodexVersion: codex.Version, WorkspaceRoots: workspaceRoots,
		Ports: ports, Toolchain: toolchain,
	}
	if err := saveConfig(paths, config); err != nil {
		return err
	}
	if options.Repair && hasExisting && options.Start {
		if err := ensureExternalMaintenance(existing); err != nil {
			return err
		}
		if err := stopAll(ctx, existing); err != nil {
			return err
		}
	}
	if err := writeLaunchAgents(paths, layout); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "Codex: %s (%s)\n", config.CodexBinary, config.CodexVersion)
	fmt.Fprintf(stdout, "Workspace roots: %s\n", strings.Join(config.WorkspaceRoots, ", "))
	fmt.Fprintf(stdout, "Ports: gateway=%d run-server=%d auth=%d postgres=%d valkey=%d\n",
		ports.Gateway, ports.RunServer, ports.AuthControl, ports.Postgres, ports.Valkey)
	if !options.Start {
		fmt.Fprintln(stdout, "Setup complete. Services were not started; run codex-remote start.")
		return nil
	}
	if err := startAll(ctx, paths, config); err != nil {
		return err
	}
	printReady(stdout, config)
	return nil
}

func initializePostgres(ctx context.Context, paths Paths, tools Toolchain, port int, password string) error {
	versionFile := filepath.Join(paths.PostgresData, "PG_VERSION")
	if _, err := os.Stat(versionFile); err == nil {
		return nil
	}
	if entries, err := os.ReadDir(paths.PostgresData); err == nil && len(entries) != 0 {
		return fmt.Errorf("PostgreSQL data directory is non-empty but has no PG_VERSION: %s", paths.PostgresData)
	}
	if err := os.MkdirAll(paths.PostgresData, 0o700); err != nil {
		return err
	}
	pwfile := filepath.Join(paths.RunDir, "initdb-password")
	if err := os.WriteFile(pwfile, []byte(password+"\n"), 0o600); err != nil {
		return err
	}
	defer os.Remove(pwfile)
	command := exec.CommandContext(ctx, tools.InitDB,
		"-D", paths.PostgresData, "-U", "codexremote", "--encoding=UTF8", "--locale=C",
		"--auth-host=scram-sha-256", "--auth-local=trust", "--pwfile="+pwfile,
	)
	if output, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("initialize PostgreSQL: %w: %s", err, strings.TrimSpace(string(output)))
	}
	postgres := exec.Command(tools.Postgres, "-D", paths.PostgresData, "-h", "127.0.0.1", "-p", strconv.Itoa(port))
	postgres.Stdout = io.Discard
	postgres.Stderr = io.Discard
	if err := postgres.Start(); err != nil {
		return fmt.Errorf("start PostgreSQL for bootstrap: %w", err)
	}
	defer func() {
		_ = postgres.Process.Signal(syscall.SIGTERM)
		_ = postgres.Wait()
	}()
	if err := waitForPostgres(ctx, tools, port, 15*time.Second); err != nil {
		return err
	}
	create := exec.CommandContext(ctx, tools.Createdb, "-h", "127.0.0.1", "-p", strconv.Itoa(port), "-U", "codexremote", "codexremote")
	create.Env = append(os.Environ(), "PGPASSWORD="+password)
	if output, err := create.CombinedOutput(); err != nil {
		return fmt.Errorf("create Codex Remote database: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func waitForPostgres(ctx context.Context, tools Toolchain, port int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		command := exec.CommandContext(ctx, tools.PGIsReady, "-h", "127.0.0.1", "-p", strconv.Itoa(port), "-U", "codexremote")
		if command.Run() == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
	return fmt.Errorf("PostgreSQL did not become ready on port %d", port)
}

func launchDomain() string { return "gui/" + strconv.Itoa(os.Getuid()) }

var serviceLoaded = func(service string) bool {
	return exec.Command("launchctl", "print", launchDomain()+"/"+serviceLabel(service)).Run() == nil
}

func startAll(ctx context.Context, paths Paths, config Config) error {
	if _, err := os.Stat(plistPath(paths, runtimeService)); os.IsNotExist(err) {
		layout, layoutErr := resolveLayout()
		if layoutErr != nil {
			return layoutErr
		}
		if layoutErr = layout.validate(); layoutErr != nil {
			return layoutErr
		}
		if layoutErr = writeLaunchAgents(paths, layout); layoutErr != nil {
			return layoutErr
		}
	} else if err != nil {
		return err
	}
	portByService := map[string][]int{
		"postgres": {config.Ports.Postgres}, "valkey": {config.Ports.Valkey},
		"relay": {config.Ports.RunServer, config.Ports.AuthControl}, "gateway": {config.Ports.Gateway},
	}
	if err := removeLegacyLaunchAgents(ctx, paths); err != nil {
		return err
	}
	loaded := serviceLoaded(runtimeService)
	if !loaded {
		for _, service := range managedServices {
			for _, port := range portByService[service] {
				if portListening(port) {
					return fmt.Errorf("port %d required by %s is occupied; no process was stopped:\n%s", port, service, listenerDescription(port))
				}
			}
		}
		command := exec.CommandContext(ctx, "launchctl", "bootstrap", launchDomain(), plistPath(paths, runtimeService))
		if output, err := command.CombinedOutput(); err != nil {
			return fmt.Errorf("start %s: %w: %s", runtimeService, err, strings.TrimSpace(string(output)))
		}
	}
	rollback := func(startErr error) error {
		if loaded {
			return startErr
		}
		rollbackCtx, cancel := context.WithTimeout(context.Background(), runtimeShutdownTimeout+5*time.Second)
		defer cancel()
		if rollbackErr := stopAll(rollbackCtx, config); rollbackErr != nil {
			return fmt.Errorf("%w; rollback runtime LaunchAgent: %v", startErr, rollbackErr)
		}
		return startErr
	}
	if err := waitForPostgres(ctx, config.Toolchain, config.Ports.Postgres, 20*time.Second); err != nil {
		return rollback(err)
	}
	if err := waitForPort(ctx, config.Ports.Valkey, 20*time.Second); err != nil {
		return rollback(fmt.Errorf("Valkey: %w", err))
	}
	if err := waitForHTTP(ctx, "http://127.0.0.1:"+strconv.Itoa(config.Ports.RunServer)+"/healthz", 30*time.Second); err != nil {
		return rollback(fmt.Errorf("Run Server: %w", err))
	}
	if err := waitForHTTP(ctx, "http://127.0.0.1:"+strconv.Itoa(config.Ports.Gateway)+"/gateway/healthz", 20*time.Second); err != nil {
		return rollback(fmt.Errorf("Gateway: %w", err))
	}
	return nil
}

func removeLegacyLaunchAgents(ctx context.Context, paths Paths) error {
	loaded := make([]string, 0, len(managedServices))
	for _, service := range managedServices {
		if serviceLoaded(service) {
			loaded = append(loaded, service)
		}
	}
	if err := stopServices(ctx, loaded); err != nil {
		return fmt.Errorf("stop legacy LaunchAgents: %w", err)
	}
	for _, service := range managedServices {
		if err := os.Remove(plistPath(paths, service)); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove legacy %s LaunchAgent: %w", service, err)
		}
	}
	return nil
}

func waitForPort(ctx context.Context, port int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if portListening(port) {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
	return fmt.Errorf("port %d did not become ready", port)
}

func waitForHTTP(ctx context.Context, endpoint string, timeout time.Duration) error {
	client := &http.Client{Timeout: 2 * time.Second}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		request, _ := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if response, err := client.Do(request); err == nil {
			_ = response.Body.Close()
			if response.StatusCode >= 200 && response.StatusCode < 300 {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
	return fmt.Errorf("health check timed out: %s", endpoint)
}

const runtimeShutdownTimeout = 20 * time.Second

var (
	runtimeShutdownPollInterval = 100 * time.Millisecond
	snapshotServiceProcessIDs   = serviceProcessIDs
	processIsAlive              = processAlive
	runtimePortListening        = portListening
)

func stopAll(ctx context.Context, config Config) error {
	launchAgents := allLaunchAgentServices()
	loaded := make([]string, 0, len(launchAgents))
	trackedProcesses := make(map[int]bool)
	for _, service := range launchAgents {
		if serviceLoaded(service) {
			loaded = append(loaded, service)
			for _, pid := range snapshotServiceProcessIDs(service) {
				trackedProcesses[pid] = true
			}
		}
	}
	if len(loaded) == 0 {
		return nil
	}
	if err := stopServices(ctx, loaded); err != nil {
		return err
	}
	processIDs := make([]int, 0, len(trackedProcesses))
	for pid := range trackedProcesses {
		processIDs = append(processIDs, pid)
	}
	slices.Sort(processIDs)
	return waitForRuntimeShutdown(ctx, processIDs, configuredRuntimePorts(config), runtimeShutdownTimeout)
}

func waitForRuntimeShutdown(ctx context.Context, processIDs, ports []int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		loaded := serviceLoaded(runtimeService)
		remainingProcesses := aliveProcessIDs(processIDs)
		remainingPorts := listeningPorts(ports)
		if !loaded && len(remainingProcesses) == 0 && len(remainingPorts) == 0 {
			return nil
		}
		if time.Now().After(deadline) {
			return runtimeShutdownError(loaded, remainingProcesses, remainingPorts)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(runtimeShutdownPollInterval):
		}
	}
}

func configuredRuntimePorts(config Config) []int {
	return []int{config.Ports.Gateway, config.Ports.RunServer, config.Ports.AuthControl, config.Ports.Postgres, config.Ports.Valkey}
}

func aliveProcessIDs(processIDs []int) []int {
	remaining := make([]int, 0, len(processIDs))
	for _, pid := range processIDs {
		if processIsAlive(pid) {
			remaining = append(remaining, pid)
		}
	}
	return remaining
}

func listeningPorts(ports []int) []int {
	remaining := make([]int, 0, len(ports))
	for _, port := range ports {
		if runtimePortListening(port) {
			remaining = append(remaining, port)
		}
	}
	return remaining
}

func runtimeShutdownError(loaded bool, processIDs, ports []int) error {
	remaining := make([]string, 0, 3)
	if loaded {
		remaining = append(remaining, serviceLabel(runtimeService)+" LaunchAgent")
	}
	if len(processIDs) != 0 {
		remaining = append(remaining, fmt.Sprintf("process PIDs %v", processIDs))
	}
	if len(ports) != 0 {
		remaining = append(remaining, fmt.Sprintf("listening ports %v", ports))
	}
	return fmt.Errorf("Codex Remote shutdown did not complete before restart; still present: %s", strings.Join(remaining, ", "))
}

func serviceProcessIDs(service string) []int {
	output, err := exec.Command("launchctl", "print", launchDomain()+"/"+serviceLabel(service)).Output()
	if err != nil {
		return nil
	}
	root := parseLaunchctlPID(string(output))
	if root == 0 {
		return nil
	}
	processOutput, err := exec.Command("ps", "-axo", "pid=,ppid=").Output()
	if err != nil {
		return []int{root}
	}
	return descendantProcessIDs(root, string(processOutput))
}

func parseLaunchctlPID(output string) int {
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 3 && fields[0] == "pid" && fields[1] == "=" {
			pid, _ := strconv.Atoi(fields[2])
			return pid
		}
	}
	return 0
}

func descendantProcessIDs(root int, output string) []int {
	children := make(map[int][]int)
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		pid, pidErr := strconv.Atoi(fields[0])
		parent, parentErr := strconv.Atoi(fields[1])
		if pidErr == nil && parentErr == nil {
			children[parent] = append(children[parent], pid)
		}
	}
	result := []int{root}
	for index := 0; index < len(result); index++ {
		result = append(result, children[result[index]]...)
	}
	slices.Sort(result)
	return result
}

func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

var bootoutService = func(ctx context.Context, service string) error {
	command := exec.CommandContext(ctx, "launchctl", "bootout", launchDomain()+"/"+serviceLabel(service))
	if output, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func stopServices(ctx context.Context, loaded []string) error {
	var failures []string
	for index := len(loaded) - 1; index >= 0; index-- {
		service := loaded[index]
		if err := bootoutService(ctx, service); err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", service, err))
		}
	}
	if len(failures) != 0 {
		return errors.New(strings.Join(failures, "; "))
	}
	return nil
}

func printReady(stdout io.Writer, config Config) {
	lan := detectLANIPv4()
	if lan == "" {
		lan = "127.0.0.1"
	}
	fmt.Fprintf(stdout, "Codex Remote is ready: http://%s:%d/\n", lan, config.Ports.Gateway)
	fmt.Fprintln(stdout, "Run codex-remote pair to generate a one-time QR code.")
}

func detectLANIPv4() string {
	route, err := exec.LookPath("route")
	if err != nil {
		return ""
	}
	output, err := exec.Command(route, "-n", "get", "default").Output()
	if err != nil {
		return ""
	}
	var networkInterface string
	fields := strings.Fields(string(output))
	for index := 0; index+1 < len(fields); index++ {
		if fields[index] == "interface:" {
			networkInterface = fields[index+1]
			break
		}
	}
	if networkInterface == "" {
		return ""
	}
	ipconfig, err := exec.LookPath("ipconfig")
	if err != nil {
		return ""
	}
	ip, err := exec.Command(ipconfig, "getifaddr", networkInterface).Output()
	if err != nil {
		return ""
	}
	value := strings.TrimSpace(string(ip))
	if parsed := net.ParseIP(value); parsed == nil || parsed.To4() == nil {
		return ""
	}
	return value
}

type statusReport struct {
	Configured bool              `json:"configured"`
	Version    string            `json:"version,omitempty"`
	Services   map[string]string `json:"services"`
	LANURL     string            `json:"lanUrl,omitempty"`
}

func status(paths Paths, config Config) statusReport {
	report := statusReport{Configured: true, Version: config.RuntimeVersion, Services: make(map[string]string)}
	for _, service := range []string{runtimeService} {
		state := "stopped"
		if serviceLoaded(service) {
			state = "loaded"
		}
		report.Services[service] = state
	}
	lan := detectLANIPv4()
	if lan == "" {
		lan = "127.0.0.1"
	}
	report.LANURL = "http://" + net.JoinHostPort(lan, strconv.Itoa(config.Ports.Gateway)) + "/"
	return report
}

func printStatus(stdout io.Writer, report statusReport, asJSON bool) error {
	if asJSON {
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(report)
	}
	fmt.Fprintf(stdout, "Runtime: %s\n", report.Version)
	for _, service := range []string{runtimeService} {
		fmt.Fprintf(stdout, "%-10s %s\n", service+":", report.Services[service])
	}
	fmt.Fprintf(stdout, "LAN URL: %s\n", report.LANURL)
	return nil
}
