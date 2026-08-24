package runtime

import (
	"fmt"
	"html"
	"os"
	"path/filepath"
	"strings"
)

const runtimeService = "runtime"

var managedServices = []string{"postgres", "valkey", "relay", "mac-agent", "gateway"}

func allLaunchAgentServices() []string {
	result := make([]string, 0, len(managedServices)+1)
	result = append(result, managedServices...)
	return append(result, runtimeService)
}

func serviceLabel(service string) string { return "com.codex-remote." + service }

func plistPath(paths Paths, service string) string {
	return filepath.Join(paths.LaunchAgents, serviceLabel(service)+".plist")
}

func writeLaunchAgents(paths Paths, layout Layout) error {
	if err := os.MkdirAll(paths.LaunchAgents, 0o755); err != nil {
		return err
	}
	label := serviceLabel(runtimeService)
	contents := renderPlist(label, layout.CLI, []string{"service-run", runtimeService}, paths.StateDir,
		filepath.Join(paths.LogDir, runtimeService+".stdout.log"), filepath.Join(paths.LogDir, runtimeService+".stderr.log"),
		map[string]string{"CODEX_REMOTE_HOME": paths.StateDir})
	path := plistPath(paths, runtimeService)
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

func renderPlist(label, program string, arguments []string, workingDirectory, stdout, stderr string, environment map[string]string) string {
	var argumentXML strings.Builder
	argumentXML.WriteString("    <string>" + html.EscapeString(program) + "</string>\n")
	for _, argument := range arguments {
		argumentXML.WriteString("    <string>" + html.EscapeString(argument) + "</string>\n")
	}
	var environmentXML strings.Builder
	if len(environment) != 0 {
		environmentXML.WriteString("  <key>EnvironmentVariables</key>\n  <dict>\n")
		for key, value := range environment {
			environmentXML.WriteString("    <key>" + html.EscapeString(key) + "</key><string>" + html.EscapeString(value) + "</string>\n")
		}
		environmentXML.WriteString("  </dict>\n")
	}
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>%s</string>
  <key>ProgramArguments</key>
  <array>
%s  </array>
  <key>WorkingDirectory</key><string>%s</string>
%s  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key>
  <dict><key>SuccessfulExit</key><false/></dict>
  <key>ThrottleInterval</key><integer>5</integer>
  <key>StandardOutPath</key><string>%s</string>
  <key>StandardErrorPath</key><string>%s</string>
</dict>
</plist>
`, html.EscapeString(label), argumentXML.String(), html.EscapeString(workingDirectory), environmentXML.String(), html.EscapeString(stdout), html.EscapeString(stderr))
}
