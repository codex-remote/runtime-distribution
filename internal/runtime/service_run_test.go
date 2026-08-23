package runtime

import (
	"strings"
	"testing"
)

func TestPostgresArgvZeroUsesResolvedExecutable(t *testing.T) {
	config := Config{Toolchain: Toolchain{Postgres: "/opt/homebrew/opt/postgresql@17/bin/postgres"}, Ports: Ports{Postgres: 54330}}
	arguments := postgresArguments(config, Paths{PostgresData: "/state/postgres"})
	if arguments[0] != config.Toolchain.Postgres {
		t.Fatalf("argv[0] = %q, want resolved executable %q", arguments[0], config.Toolchain.Postgres)
	}
}

func TestPostgresEnvironmentDefinesLaunchdSafeLocale(t *testing.T) {
	t.Setenv("LC_ALL", "zh_CN.UTF-8")
	count := 0
	for _, value := range postgresEnvironment() {
		if strings.HasPrefix(value, "LC_ALL=") {
			count++
			if value != "LC_ALL=C" {
				t.Fatalf("LC_ALL = %q, want C", value)
			}
		}
	}
	if count != 1 {
		t.Fatalf("LC_ALL entries = %d, want 1", count)
	}
}
