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
		repair := flags.Bool("repair", false, "repair an existing installation without changing persisted ports")
		noStart := flags.Bool("no-start", false, "prepare the installation without loading services")
		var workspaceRoots stringList
		flags.Var(&workspaceRoots, "workspace-root", "allowed workspace root (repeatable; defaults to current directory)")
		if flags.Parse(arguments) != nil {
			return 2
		}
		err = setup(ctx, version, setupOptions{CodexBinary: *codexBinary, WorkspaceRoots: workspaceRoots, Repair: *repair, Start: !*noStart}, stdout)
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
			err = stopAll(ctx)
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
			err = stopAll(ctx)
		}
		if err == nil {
			err = startAll(ctx, paths, config)
		}
		if err == nil {
			printReady(stdout, config)
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
		var layout Layout
		paths, err = resolvePaths()
		if err == nil {
			config, err = loadConfig(paths)
		}
		if err == nil {
			layout, err = resolveLayout()
		}
		if err == nil {
			lan := detectLANIPv4()
			if lan == "" {
				lan = "127.0.0.1"
			}
			pairArguments := []string{
				"--control-url", "http://127.0.0.1:" + strconv.Itoa(config.Ports.AuthControl),
				"--origin", "http://" + lan + ":" + strconv.Itoa(config.Ports.Gateway),
			}
			pairArguments = append(pairArguments, arguments...)
			pair := exec.CommandContext(ctx, layout.PairQR, pairArguments...)
			pair.Stdout, pair.Stderr, pair.Stdin = stdout, stderr, os.Stdin
			err = pair.Run()
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
		err = runService(arguments[0])
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

func unexpectedArguments(stderr io.Writer, command string) int {
	fmt.Fprintf(stderr, "%s does not accept arguments\n", command)
	return 2
}

func printUsage(writer io.Writer) {
	fmt.Fprintln(writer, `Codex Remote

Usage:
  codex-remote setup [--codex-binary PATH] [--workspace-root DIR ...] [--repair] [--no-start]
  codex-remote start | stop | restart | status [--json]
  codex-remote pair [pairqr options]
  codex-remote doctor [--json]
  codex-remote migrate | backup
  codex-remote rollback --backup FILE --yes
  codex-remote uninstall [--purge --yes]
  codex-remote version`)
}
