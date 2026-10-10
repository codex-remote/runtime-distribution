package runtime

import (
	"bytes"
	"strings"
	"testing"
)

func TestParseTailnetStatusUsesSelfIPv4AndNormalizesDNSName(t *testing.T) {
	status, err := parseTailnetStatus([]byte(`{
        "BackendState":"Running",
        "TailscaleIPs":["100.90.0.1"],
		"CurrentTailnet":{"MagicDNSEnabled":true},
		"Self":{"DNSName":"home-mac.example.ts.net.","TailscaleIPs":["fd7a:115c:a1e0::1","100.88.10.20"]},
		"Peer":{"nodekey:one":{"ID":"device-one","DNSName":"iphone-14-pro.example.ts.net.","TailscaleIPs":["100.79.108.68"],"OS":"iOS","Online":true}}
    }`))
	if err != nil {
		t.Fatal(err)
	}
	if status.IPv4 != "100.88.10.20" || status.DNSName != "home-mac.example.ts.net" || status.MagicDNSName != "home-mac" {
		t.Fatalf("tailnet status = %#v", status)
	}
	if len(status.Peers) != 1 || status.Peers[0].Name != "iphone-14-pro" {
		t.Fatalf("tailnet peers = %#v", status.Peers)
	}
}

func TestParseTailnetStatusRejectsDisconnectedClient(t *testing.T) {
	_, err := parseTailnetStatus([]byte(`{"BackendState":"Stopped"}`))
	if err == nil {
		t.Fatal("disconnected Tailscale status was accepted")
	}
}

func TestNetworkOriginSelectsRequestedNetwork(t *testing.T) {
	tailnet := tailnetStatus{IPv4: "100.88.10.20", DNSName: "home-mac.example.ts.net"}
	lan, err := networkOrigin(networkModeLAN, "192.168.0.114", tailnet, 18774, "")
	if err != nil || lan != "http://192.168.0.114:18774" {
		t.Fatalf("LAN origin = %q, %v", lan, err)
	}
	tailnet.MagicDNSEnabled = true
	tailnet.MagicDNSName = "home-mac"
	remote, err := networkOrigin(networkModeTailscale, "192.168.0.114", tailnet, 18774, "")
	if err != nil || remote != "http://home-mac:18774" {
		t.Fatalf("Tailscale origin = %q, %v", remote, err)
	}
	remoteIP, err := networkOrigin(networkModeTailscale, "192.168.0.114", tailnet, 18774, "ip")
	if err != nil || remoteIP != "http://100.88.10.20:18774" {
		t.Fatalf("Tailscale IP origin = %q, %v", remoteIP, err)
	}
}

func TestMissingNetworkModeDefaultsToLAN(t *testing.T) {
	if got := effectiveNetworkMode(Config{}); got != networkModeLAN {
		t.Fatalf("effective mode = %q, want lan", got)
	}
}

func TestChooseTailnetPeerAutomaticallyUsesOnlyOnlineIOSDevice(t *testing.T) {
	tailnet := tailnetStatus{Peers: []tailnetPeer{
		{Name: "offline-iphone", OS: "iOS", Online: false},
		{Name: "home-ipad", DNSName: "home-ipad.example.ts.net", IPv4: "100.70.0.2", OS: "iOS", Online: true},
		{Name: "linux-server", OS: "linux", Online: true},
	}}
	peer, err := chooseTailnetPeer(tailnet, "", strings.NewReader(""), &bytes.Buffer{}, false)
	if err != nil {
		t.Fatal(err)
	}
	if peer.Name != "home-ipad" {
		t.Fatalf("selected peer = %#v", peer)
	}
}

func TestChooseTailnetPeerRequiresSelectorWithoutTerminal(t *testing.T) {
	tailnet := tailnetStatus{Peers: []tailnetPeer{
		{Name: "iphone-one", IPv4: "100.70.0.2", OS: "iOS", Online: true},
		{Name: "iphone-two", IPv4: "100.70.0.3", OS: "iOS", Online: true},
	}}
	if _, err := chooseTailnetPeer(tailnet, "", strings.NewReader(""), &bytes.Buffer{}, false); err == nil {
		t.Fatal("multiple devices were accepted without a selector")
	}
	peer, err := chooseTailnetPeer(tailnet, "100.70.0.3", strings.NewReader(""), &bytes.Buffer{}, false)
	if err != nil {
		t.Fatal(err)
	}
	if peer.Name != "iphone-two" {
		t.Fatalf("selected peer = %#v", peer)
	}
}

func TestChooseTailnetPeerPromptsWhenInteractive(t *testing.T) {
	tailnet := tailnetStatus{Peers: []tailnetPeer{
		{Name: "iphone-one", IPv4: "100.70.0.2", OS: "iOS", Online: true},
		{Name: "iphone-two", IPv4: "100.70.0.3", OS: "iOS", Online: true},
	}}
	output := &bytes.Buffer{}
	peer, err := chooseTailnetPeer(tailnet, "", strings.NewReader("2\n"), output, true)
	if err != nil {
		t.Fatal(err)
	}
	if peer.Name != "iphone-two" || !strings.Contains(output.String(), "1. iphone-one") {
		t.Fatalf("selected peer = %#v, output = %q", peer, output.String())
	}
}
