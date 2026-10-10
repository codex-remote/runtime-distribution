package runtime

import (
	"reflect"
	"testing"
)

func TestPairingArgumentsKeepTerminalAndLinkDefaults(t *testing.T) {
	config := Config{Ports: Ports{Gateway: 18774, AuthControl: 18776}}
	got := pairingArguments(config, "http://192.168.0.185:18774", "", nil)
	want := []string{
		"--control-url", "http://127.0.0.1:18776",
		"--origin", "http://192.168.0.185:18774",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("pairingArguments() = %#v, want %#v", got, want)
	}
}

func TestPairingArgumentsPreserveExplicitOverrides(t *testing.T) {
	config := Config{Ports: Ports{Gateway: 18774, AuthControl: 18776}}
	got := pairingArguments(config, "http://192.168.0.185:18774", "iphone-14-pro", []string{"--terminal=false", "--output", "/tmp/pair.png"})
	if !reflect.DeepEqual(got[6:], []string{"--terminal=false", "--output", "/tmp/pair.png"}) {
		t.Fatalf("pairingArguments() lost explicit options: %#v", got)
	}
}

func TestParsePairOptionsSeparatesRuntimeAndPairQRFlags(t *testing.T) {
	got, err := parsePairOptions([]string{"--network=tailscale", "--address", "ip", "--device", "iphone", "--terminal=false"})
	if err != nil {
		t.Fatal(err)
	}
	want := pairOptions{Network: "tailscale", Address: "ip", Device: "iphone", PairQRArguments: []string{"--terminal=false"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("pair options = %#v, want %#v", got, want)
	}
}
