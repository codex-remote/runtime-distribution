package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	goruntime "runtime"
	"strconv"
	"strings"
	"time"
)

type doctorCheck struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

type doctorReport struct {
	OK     bool          `json:"ok"`
	Checks []doctorCheck `json:"checks"`
}

func runDoctor(ctx context.Context, stdout io.Writer, asJSON bool) error {
	report := doctorReport{OK: true}
	add := func(name string, err error, success string) {
		check := doctorCheck{Name: name, Status: "ok", Message: success}
		if err != nil {
			check.Status = "error"
			check.Message = err.Error()
			report.OK = false
		}
		report.Checks = append(report.Checks, check)
	}
	platform := goruntime.GOOS + "-" + goruntime.GOARCH
	if platform != "darwin-arm64" {
		add("platform", fmt.Errorf("unsupported platform %s", platform), "")
	} else {
		add("platform", nil, platform)
	}
	paths, err := resolvePaths()
	add("paths", err, paths.StateDir)
	if err == nil {
		config, configErr := loadConfig(paths)
		add("configuration", configErr, paths.ConfigFile)
		if configErr == nil {
			layout, layoutErr := resolveLayout()
			if layoutErr == nil {
				layoutErr = layout.validate()
			}
			add("runtime-layout", layoutErr, "all runtime files are present")
			codexCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
			codexCheck, codexErr := validateCodex(codexCtx, config.CodexBinary)
			cancel()
			add("codex", codexErr, codexCheck.Version+" at "+codexCheck.Binary)
			_, postgresErr := readSecret(ctx, postgresSecretService)
			add("postgres-keychain", postgresErr, "credential is present")
			_, valkeyErr := readSecret(ctx, valkeySecretService)
			add("valkey-keychain", valkeyErr, "credential is present")
			var runtimeErr error
			if !serviceLoaded(runtimeService) {
				runtimeErr = fmt.Errorf("LaunchAgent is not loaded")
			}
			add("service-runtime", runtimeErr, "loaded")
			var legacy []string
			for _, service := range managedServices {
				if serviceLoaded(service) {
					legacy = append(legacy, service)
				}
			}
			var legacyErr error
			if len(legacy) != 0 {
				legacyErr = fmt.Errorf("legacy LaunchAgents are loaded (%s); run setup --repair", strings.Join(legacy, ", "))
			}
			add("legacy-launchagents", legacyErr, "none loaded")
			add("run-server-health", healthError("http://127.0.0.1:"+strconv.Itoa(config.Ports.RunServer)+"/healthz"), "healthy")
			add("gateway-health", healthError("http://127.0.0.1:"+strconv.Itoa(config.Ports.Gateway)+"/gateway/healthz"), "healthy")
		}
	}
	if asJSON {
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		_ = encoder.Encode(report)
	} else {
		for _, check := range report.Checks {
			fmt.Fprintf(stdout, "%-24s %-5s %s\n", check.Name, check.Status, check.Message)
		}
	}
	if !report.OK {
		return fmt.Errorf("doctor found one or more problems")
	}
	return nil
}

func healthError(endpoint string) error {
	client := &http.Client{Timeout: 2 * time.Second}
	response, err := client.Get(endpoint)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d", response.StatusCode)
	}
	return nil
}
