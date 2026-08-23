package runtime

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const configSchemaVersion = 1

type Ports struct {
	Gateway     int `json:"gateway"`
	RunServer   int `json:"runServer"`
	AuthControl int `json:"authControl"`
	Postgres    int `json:"postgres"`
	Valkey      int `json:"valkey"`
}

type Toolchain struct {
	InitDB    string `json:"initdb"`
	Postgres  string `json:"postgres"`
	PGIsReady string `json:"pgIsReady"`
	Createdb  string `json:"createdb"`
	Dropdb    string `json:"dropdb"`
	PGDump    string `json:"pgDump"`
	PGRestore string `json:"pgRestore"`
	PSQL      string `json:"psql"`
}

type Config struct {
	SchemaVersion  int       `json:"schemaVersion"`
	RuntimeVersion string    `json:"runtimeVersion"`
	InstalledAt    time.Time `json:"installedAt"`
	CodexBinary    string    `json:"codexBinary"`
	CodexVersion   string    `json:"codexVersion"`
	WorkspaceRoots []string  `json:"workspaceRoots"`
	Ports          Ports     `json:"ports"`
	Toolchain      Toolchain `json:"toolchain"`
}

type Paths struct {
	StateDir     string
	ConfigFile   string
	DataDir      string
	PostgresData string
	RunDir       string
	LogDir       string
	BackupDir    string
	LaunchAgents string
	LockDir      string
}

func resolvePaths() (Paths, error) {
	stateDir := os.Getenv("CODEX_REMOTE_HOME")
	if stateDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return Paths{}, fmt.Errorf("resolve home directory: %w", err)
		}
		stateDir = filepath.Join(home, "Library", "Application Support", "CodexRemote")
	}
	stateDir, err := filepath.Abs(stateDir)
	if err != nil {
		return Paths{}, fmt.Errorf("resolve state directory: %w", err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return Paths{}, fmt.Errorf("resolve home directory: %w", err)
	}
	return Paths{
		StateDir:     stateDir,
		ConfigFile:   filepath.Join(stateDir, "config.json"),
		DataDir:      filepath.Join(stateDir, "Data"),
		PostgresData: filepath.Join(stateDir, "Data", "postgres"),
		RunDir:       filepath.Join(stateDir, "Run"),
		LogDir:       filepath.Join(stateDir, "Logs"),
		BackupDir:    filepath.Join(stateDir, "Backups"),
		LaunchAgents: filepath.Join(home, "Library", "LaunchAgents"),
		LockDir:      filepath.Join(stateDir, ".operation.lock"),
	}, nil
}

func loadConfig(paths Paths) (Config, error) {
	contents, err := os.ReadFile(paths.ConfigFile)
	if err != nil {
		if os.IsNotExist(err) {
			return Config{}, fmt.Errorf("Codex Remote is not set up; run codex-remote setup")
		}
		return Config{}, err
	}
	var config Config
	if err := json.Unmarshal(contents, &config); err != nil {
		return Config{}, fmt.Errorf("decode %s: %w", paths.ConfigFile, err)
	}
	if config.SchemaVersion != configSchemaVersion {
		return Config{}, fmt.Errorf("unsupported config schema %d", config.SchemaVersion)
	}
	return config, nil
}

func saveConfig(paths Paths, config Config) error {
	contents, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	contents = append(contents, '\n')
	temporary := paths.ConfigFile + ".tmp"
	if err := os.WriteFile(temporary, contents, 0o600); err != nil {
		return err
	}
	if err := os.Rename(temporary, paths.ConfigFile); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	return nil
}
