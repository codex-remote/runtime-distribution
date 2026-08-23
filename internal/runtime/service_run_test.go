package runtime

import "testing"

func TestPostgresArgvZeroUsesResolvedExecutable(t *testing.T) {
	config := Config{Toolchain: Toolchain{Postgres: "/opt/homebrew/opt/postgresql@17/bin/postgres"}, Ports: Ports{Postgres: 54330}}
	arguments := postgresArguments(config, Paths{PostgresData: "/state/postgres"})
	if arguments[0] != config.Toolchain.Postgres {
		t.Fatalf("argv[0] = %q, want resolved executable %q", arguments[0], config.Toolchain.Postgres)
	}
}
