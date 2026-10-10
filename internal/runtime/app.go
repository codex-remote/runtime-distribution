package runtime

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
)

type stringList []string

func (values *stringList) String() string { return strings.Join(*values, ",") }
func (values *stringList) Set(value string) error {
	*values = append(*values, value)
	return nil
}

func Run(arguments []string, version string, stdout, stderr io.Writer) int {
	if len(arguments) == 0 {
		printUsage(stderr)
		return 2
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	command := arguments[0]
	arguments = arguments[1:]
	var err error
	switch command {
	case "version", "--version", "-version":
		fmt.Fprintln(stdout, version)
		return 0
	case "help", "--help", "-h":
		printUsage(stdout)
		return 0
	case "setup":
		flags := flag.NewFlagSet("setup", flag.ContinueOnError)
		flags.SetOutput(stderr)
		codexBinary := flags.String("codex-binary", "", "explicit Codex CLI path")
		networkMode := flags.String("network", "", "access network: lan or tailscale")
		repair := flags.Bool("repair", false, "repair an existing installation without changing persisted ports")
		noStart := flags.Bool("no-start", false, "prepare the installation without loading services")
		var workspaceRoots stringList
		flags.Var(&workspaceRoots, "workspace-root", "allowed workspace root (repeatable; defaults to current directory)")
		if flags.Parse(arguments) != nil {
			return 2
		}
		err = setup(ctx, version, setupOptions{CodexBinary: *codexBinary, WorkspaceRoots: workspaceRoots, NetworkMode: *networkMode, Repair: *repair, Start: !*noStart}, stdout)
	case "start":
		if len(arguments) != 0 {
			return unexpectedArguments(stderr, command)
		}
		var paths Paths
		var config Config
		paths, err = resolvePaths()
		if err == nil {
			config, err = loadConfig(paths)
		}
		if err == nil {
			err = startAll(ctx, paths, config)
		}
		if err == nil {
			printReady(stdout, config)
			if pairErr := runPairQR(ctx, config, nil, stdout, stderr); pairErr != nil {
				fmt.Fprintf(stderr, "warning: generate pairing QR: %v\n", pairErr)
			}
		}
	case "stop":
		if len(arguments) != 0 {
			return unexpectedArguments(stderr, command)
		}
		var paths Paths
		var config Config
		paths, err = resolvePaths()
		if err == nil {
			config, err = loadConfig(paths)
		}
		if err == nil {
			err = ensureExternalMaintenance(config)
		}
		if err == nil {
			err = stopAll(ctx, config)
		}
		if err == nil {
			fmt.Fprintln(stdout, "Codex Remote services stopped.")
		}
	case "restart":
		if len(arguments) != 0 {
			return unexpectedArguments(stderr, command)
		}
		var paths Paths
		var config Config
		paths, err = resolvePaths()
		if err == nil {
			config, err = loadConfig(paths)
		}
		if err == nil {
			err = ensureExternalMaintenance(config)
		}
		if err == nil {
			err = stopAll(ctx, config)
		}
		if err == nil {
			err = startAll(ctx, paths, config)
		}
		if err == nil {
			printReady(stdout, config)
			if pairErr := runPairQR(ctx, config, nil, stdout, stderr); pairErr != nil {
				fmt.Fprintf(stderr, "warning: generate pairing QR: %v\n", pairErr)
			}
		}
	case "status":
		flags := flag.NewFlagSet("status", flag.ContinueOnError)
		flags.SetOutput(stderr)
		asJSON := flags.Bool("json", false, "print machine-readable JSON")
		if flags.Parse(arguments) != nil {
			return 2
		}
		var paths Paths
		var config Config
		paths, err = resolvePaths()
		if err == nil {
			config, err = loadConfig(paths)
		}
		if err == nil {
			err = printStatus(stdout, status(paths, config), *asJSON)
		}
	case "pair":
		var paths Paths
		var config Config
		paths, err = resolvePaths()
		if err == nil {
			config, err = loadConfig(paths)
		}
		if err == nil {
			err = runPairQR(ctx, config, arguments, stdout, stderr)
		}
	case "network":
		if len(arguments) > 1 {
			fmt.Fprintln(stderr, "network accepts at most one mode: lan or tailscale")
			return 2
		}
		var paths Paths
		var config Config
		paths, err = resolvePaths()
		if err == nil {
			config, err = loadConfig(paths)
		}
		if err == nil && len(arguments) == 0 {
			err = printStatus(stdout, status(paths, config), false)
		}
		if err == nil && len(arguments) == 1 {
			err = setNetworkMode(paths, &config, arguments[0], stdout)
		}
	case "doctor":
		flags := flag.NewFlagSet("doctor", flag.ContinueOnError)
		flags.SetOutput(stderr)
		asJSON := flags.Bool("json", false, "print machine-readable JSON")
		if flags.Parse(arguments) != nil {
			return 2
		}
		err = runDoctor(ctx, stdout, *asJSON)
	case "migrate":
		if len(arguments) != 0 {
			return unexpectedArguments(stderr, command)
		}
		var paths Paths
		var config Config
		paths, err = resolvePaths()
		if err == nil {
			config, err = loadConfig(paths)
		}
		if err == nil {
			err = migrate(ctx, paths, config, stdout)
		}
	case "backup":
		if len(arguments) != 0 {
			return unexpectedArguments(stderr, command)
		}
		var paths Paths
		var config Config
		paths, err = resolvePaths()
		if err == nil {
			config, err = loadConfig(paths)
		}
		if err == nil {
			_, err = backupDatabase(ctx, paths, config, stdout)
		}
	case "rollback":
		flags := flag.NewFlagSet("rollback", flag.ContinueOnError)
		flags.SetOutput(stderr)
		backup := flags.String("backup", "", "database backup created by codex-remote backup")
		confirmed := flags.Bool("yes", false, "confirm database replacement")
		if flags.Parse(arguments) != nil {
			return 2
		}
		if strings.TrimSpace(*backup) == "" {
			fmt.Fprintln(stderr, "rollback requires --backup <file>")
			return 2
		}
		var paths Paths
		var config Config
		paths, err = resolvePaths()
		if err == nil {
			config, err = loadConfig(paths)
		}
		if err == nil {
			err = restoreDatabase(ctx, paths, config, *backup, *confirmed, stdout)
		}
	case "uninstall":
		flags := flag.NewFlagSet("uninstall", flag.ContinueOnError)
		flags.SetOutput(stderr)
		purge := flags.Bool("purge", false, "permanently remove Codex Remote data and Keychain credentials")
		confirmed := flags.Bool("yes", false, "confirm permanent deletion with --purge")
		if flags.Parse(arguments) != nil {
			return 2
		}
		var paths Paths
		var config Config
		paths, err = resolvePaths()
		if err == nil {
			config, err = loadConfig(paths)
		}
		if err == nil {
			err = uninstall(ctx, paths, config, *purge, *confirmed, stdout)
		}
	case "service-run":
		if len(arguments) != 1 {
			fmt.Fprintln(stderr, "service-run requires one internal service name")
			return 2
		}
		err = runService(ctx, arguments[0])
	case "dev-supervisor":
		flags := flag.NewFlagSet("dev-supervisor", flag.ContinueOnError)
		flags.SetOutput(stderr)
		options := devSupervisorOptions{}
		flags.StringVar(&options.Relay, "relay", "", "Relay executable")
		flags.StringVar(&options.Agent, "agent", "", "Mac Agent executable")
		flags.StringVar(&options.Gateway, "gateway", "", "Mobile Web Gateway executable")
		flags.StringVar(&options.StaticDir, "static", "", "Mobile Web dist directory")
		flags.StringVar(&options.CodexBinary, "codex-binary", "", "Codex executable")
		flags.StringVar(&options.RelayAddr, "relay-addr", "127.0.0.1:18875", "Relay listen address")
		flags.StringVar(&options.AuthControl, "auth-control-addr", "127.0.0.1:18876", "Auth Control listen address")
		flags.StringVar(&options.GatewayAddr, "gateway-addr", "0.0.0.0:18874", "Gateway listen address")
		flags.StringVar(&options.DatabaseURL, "database-url", "postgres://codexremote:codexremote@127.0.0.1:54329/codexremote?sslmode=disable", "Runtime PostgreSQL URL")
		flags.StringVar(&options.RedisURL, "redis-url", "redis://default:codexremote@127.0.0.1:63799/0", "Runtime Redis/Valkey URL")
		flags.StringVar(&options.LogDir, "log-dir", "", "Supervisor log directory")
		flags.Var((*stringList)(&options.WorkspaceRoots), "workspace-root", "allowed Mac Agent workspace root (repeatable)")
		if flags.Parse(arguments) != nil {
			return 2
		}
		err = runDevSupervisor(ctx, options, stdout)
	default:
		fmt.Fprintf(stderr, "unknown command %q\n\n", command)
		printUsage(stderr)
		return 2
	}
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	return 0
}

func runPairQR(ctx context.Context, config Config, arguments []string, stdout, stderr io.Writer) error {
	options, err := parsePairOptions(arguments)
	if err != nil {
		return err
	}
	layout, err := resolveLayout()
	if err != nil {
		return err
	}
	lan := detectLANIPv4()
	mode := effectiveNetworkMode(config)
	if options.Network != "" {
		mode, err = parseNetworkMode(options.Network)
		if err != nil {
			return err
		}
	}
	var tailnet tailnetStatus
	var clientName string
	if mode == networkModeTailscale {
		tailnet, err = detectTailnet()
		if err != nil {
			return err
		}
		peer, err := chooseTailnetPeer(tailnet, options.Device, os.Stdin, stdout, stdinIsTerminal())
		if err != nil {
			return err
		}
		clientName = peer.Name
		fmt.Fprintf(stdout, "Tailscale device: %s (%s)\n", peer.Name, peer.IPv4)
	}
	origin, err := networkOrigin(mode, lan, tailnet, config.Ports.Gateway, options.Address)
	if err != nil {
		return err
	}
	pairArguments := pairingArguments(config, origin, clientName, options.PairQRArguments)
	pair := exec.CommandContext(ctx, layout.PairQR, pairArguments...)
	pair.Stdout, pair.Stderr, pair.Stdin = stdout, stderr, os.Stdin
	return pair.Run()
}

type pairOptions struct {
	Network         string
	Address         string
	Device          string
	PairQRArguments []string
}

func parsePairOptions(arguments []string) (pairOptions, error) {
	options := pairOptions{}
	for index := 0; index < len(arguments); index++ {
		argument := arguments[index]
		if argument == "--" {
			options.PairQRArguments = append(options.PairQRArguments, arguments[index+1:]...)
			break
		}
		name, value, matched := strings.Cut(argument, "=")
		if name != "--network" && name != "--address" && name != "--device" {
			options.PairQRArguments = append(options.PairQRArguments, argument)
			continue
		}
		if !matched {
			index++
			if index >= len(arguments) {
				return pairOptions{}, fmt.Errorf("%s requires a value", name)
			}
			value = arguments[index]
		}
		switch name {
		case "--network":
			options.Network = value
		case "--address":
			options.Address = value
		case "--device":
			options.Device = value
		}
	}
	return options, nil
}

func stdinIsTerminal() bool {
	info, err := os.Stdin.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func pairingArguments(config Config, origin, clientName string, arguments []string) []string {
	pairArguments := []string{
		"--control-url", "http://127.0.0.1:" + strconv.Itoa(config.Ports.AuthControl),
		"--origin", origin,
	}
	if clientName != "" {
		pairArguments = append(pairArguments, "--name", clientName)
	}
	return append(pairArguments, arguments...)
}

func setNetworkMode(paths Paths, config *Config, value string, stdout io.Writer) error {
	mode, err := parseNetworkMode(value)
	if err != nil {
		return err
	}
	lan := detectLANIPv4()
	var tailnet tailnetStatus
	if mode == networkModeTailscale {
		tailnet, err = detectTailnet()
		if err != nil {
			return err
		}
	}
	origin, err := networkOrigin(mode, lan, tailnet, config.Ports.Gateway, "")
	if err != nil {
		return err
	}
	config.NetworkMode = mode
	if err := saveConfig(paths, *config); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "Network mode: %s\n", mode)
	fmt.Fprintf(stdout, "Active URL: %s/\n", origin)
	fmt.Fprintln(stdout, "Run codex-remote pair to generate a new one-time pairing link for this origin.")
	return nil
}

func unexpectedArguments(stderr io.Writer, command string) int {
	fmt.Fprintf(stderr, "%s does not accept arguments\n", command)
	return 2
}

func printUsage(writer io.Writer) {
	fmt.Fprintln(writer, `Codex Remote

Usage:
  codex-remote setup [--codex-binary PATH] [--workspace-root DIR ...] [--network lan|tailscale] [--repair] [--no-start]
  codex-remote start | stop | restart | status [--json]
  codex-remote pair [--network lan|tailscale] [--address magicdns|ip] [--device NAME] [pairqr options]
  codex-remote network [lan|tailscale]
  codex-remote doctor [--json]
  codex-remote migrate | backup
  codex-remote rollback --backup FILE --yes
  codex-remote uninstall [--purge --yes]
  codex-remote dev-supervisor --relay PATH --agent PATH --gateway PATH --static DIR --codex-binary PATH --log-dir DIR [--workspace-root DIR ...]
  codex-remote version`)
}
