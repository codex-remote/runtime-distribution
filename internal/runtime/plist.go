package runtime

import (
	"fmt"
	"html"
	"os"
	"path/filepath"
	"strings"
)

var services = []string{"postgres", "valkey", "relay", "mac-agent", "gateway"}

func serviceLabel(service string) string { return "com.codex-remote." + service }

func plistPath(paths Paths, service string) string {
	return filepath.Join(paths.LaunchAgents, serviceLabel(service)+".plist")
}

func writeLaunchAgents(paths Paths, layout Layout) error {
	if err := os.MkdirAll(paths.LaunchAgents, 0o755); err != nil {
		return err
	}
	for _, service := range services {
		label := serviceLabel(service)
		contents := renderPlist(label, layout.CLI, []string{"service-run", service}, paths.StateDir,
			filepath.Join(paths.LogDir, service+".stdout.log"), filepath.Join(paths.LogDir, service+".stderr.log"))
		path := plistPath(paths, service)
		if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
			return fmt.Errorf("write %s: %w", path, err)
		}
	}
	return nil
}

func renderPlist(label, program string, arguments []string, workingDirectory, stdout, stderr string) string {
	var argumentXML strings.Builder
	argumentXML.WriteString("    <string>" + html.EscapeString(program) + "</string>\n")
	for _, argument := range arguments {
		argumentXML.WriteString("    <string>" + html.EscapeString(argument) + "</string>\n")
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
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key>
  <dict><key>SuccessfulExit</key><false/></dict>
  <key>ThrottleInterval</key><integer>5</integer>
  <key>StandardOutPath</key><string>%s</string>
  <key>StandardErrorPath</key><string>%s</string>
</dict>
</plist>
`, html.EscapeString(label), argumentXML.String(), html.EscapeString(workingDirectory), html.EscapeString(stdout), html.EscapeString(stderr))
}
