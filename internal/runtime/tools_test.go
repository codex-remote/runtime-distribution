package runtime

import (
	"net"
	"testing"
)

func TestFindAvailablePortDoesNotDisturbForeignListener(t *testing.T) {
	listener, err := net.Listen("tcp4", "0.0.0.0:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	occupied := listener.Addr().(*net.TCPAddr).Port
	selected, err := findAvailablePort(occupied, map[int]bool{})
	if err != nil {
		t.Fatal(err)
	}
	if selected == occupied {
		t.Fatalf("selected occupied port %d", occupied)
	}
	probe, err := net.Dial("tcp4", listener.Addr().String())
	if err != nil {
		t.Fatalf("foreign listener was disturbed: %v", err)
	}
	_ = probe.Close()
}

func TestSelectPortsReturnsDistinctAvailablePorts(t *testing.T) {
	ports, err := selectPorts()
	if err != nil {
		t.Fatal(err)
	}
	values := []int{ports.Gateway, ports.RunServer, ports.AuthControl, ports.Postgres, ports.Valkey}
	seen := make(map[int]bool)
	for _, value := range values {
		if seen[value] {
			t.Fatalf("port %d selected more than once", value)
		}
		seen[value] = true
	}
}
