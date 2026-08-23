package runtime

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

type Layout struct {
	CLI       string
	Relay     string
	Relayctl  string
	PairQR    string
	MacAgent  string
	Gateway   string
	MobileWeb string
}

func resolveLayout() (Layout, error) {
	executable, err := os.Executable()
	if err != nil {
		return Layout{}, err
	}
	resolved, err := filepath.EvalSymlinks(executable)
	if err != nil {
		return Layout{}, err
	}
	binDir := filepath.Dir(resolved)
	cli := stableCLIPath(resolved)
	staticCandidates := []string{
		filepath.Clean(filepath.Join(binDir, "..", "share", "codex-remote", "mobile-web")),
		filepath.Clean(filepath.Join(binDir, "..", "share", "mobile-web")),
	}
	var staticDir string
	for _, candidate := range staticCandidates {
		if info, statErr := os.Stat(filepath.Join(candidate, "index.html")); statErr == nil && !info.IsDir() {
			staticDir = candidate
			break
		}
	}
	layout := Layout{
		CLI:       cli,
		Relay:     filepath.Join(binDir, "relay-server"),
		Relayctl:  filepath.Join(binDir, "relayctl"),
		PairQR:    filepath.Join(binDir, "pairqr"),
		MacAgent:  filepath.Join(binDir, "mac-agent"),
		Gateway:   filepath.Join(binDir, "mobile-web-gateway"),
		MobileWeb: staticDir,
	}
	return layout, nil
}

func stableCLIPath(resolved string) string {
	if candidate, err := exec.LookPath("codex-remote"); err == nil {
		if absolute, absErr := filepath.Abs(candidate); absErr == nil {
			if target, evalErr := filepath.EvalSymlinks(absolute); evalErr == nil && target == resolved {
				return absolute
			}
		}
	}
	return resolved
}

func (layout Layout) validate() error {
	for label, path := range map[string]string{
		"Codex Remote CLI":   layout.CLI,
		"Relay Server":       layout.Relay,
		"relayctl":           layout.Relayctl,
		"pairqr":             layout.PairQR,
		"Mac Agent":          layout.MacAgent,
		"Mobile Web Gateway": layout.Gateway,
	} {
		if _, err := resolveExecutable(path); err != nil {
			return fmt.Errorf("%s is missing or not executable at %s: %w", label, path, err)
		}
	}
	if layout.MobileWeb == "" {
		return fmt.Errorf("Mobile Web assets are missing next to the installed runtime")
	}
	return nil
}
