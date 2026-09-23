package runtime

import "testing"

func TestHasPairDisplayOverride(t *testing.T) {
	for _, arguments := range [][]string{
		{"--terminal"},
		{"--terminal=false"},
		{"--terminal=true"},
		{"--output", "/tmp/pair.png"},
		{"--output=/tmp/pair.png"},
	} {
		if !hasPairDisplayOverride(arguments) {
			t.Fatalf("hasPairDisplayOverride(%v) = false", arguments)
		}
	}
	for _, arguments := range [][]string{
		nil,
		{"--name", "iPhone"},
		{"--print-link=false"},
	} {
		if hasPairDisplayOverride(arguments) {
			t.Fatalf("hasPairDisplayOverride(%v) = true", arguments)
		}
	}
}
