package runtime

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
)

type NetworkMode string

const (
	networkModeLAN         NetworkMode = "lan"
	networkModeTailscale   NetworkMode = "tailscale"
	tailnetAddressMagicDNS             = "magicdns"
	tailnetAddressIP                   = "ip"
)

type tailnetStatus struct {
	IPv4            string
	DNSName         string
	MagicDNSName    string
	MagicDNSEnabled bool
	Peers           []tailnetPeer
}

type tailnetPeer struct {
	ID      string
	Name    string
	DNSName string
	IPv4    string
	OS      string
	Online  bool
	Active  bool
}

type tailscaleStatusDocument struct {
	BackendState string   `json:"BackendState"`
	TailscaleIPs []string `json:"TailscaleIPs"`
	Self         struct {
		DNSName      string   `json:"DNSName"`
		TailscaleIPs []string `json:"TailscaleIPs"`
	} `json:"Self"`
	CurrentTailnet struct {
		MagicDNSEnabled bool `json:"MagicDNSEnabled"`
	} `json:"CurrentTailnet"`
	Peer map[string]struct {
		ID           string   `json:"ID"`
		HostName     string   `json:"HostName"`
		DNSName      string   `json:"DNSName"`
		TailscaleIPs []string `json:"TailscaleIPs"`
		OS           string   `json:"OS"`
		Online       bool     `json:"Online"`
		Active       bool     `json:"Active"`
	} `json:"Peer"`
}

func parseNetworkMode(value string) (NetworkMode, error) {
	switch NetworkMode(strings.ToLower(strings.TrimSpace(value))) {
	case networkModeLAN:
		return networkModeLAN, nil
	case networkModeTailscale:
		return networkModeTailscale, nil
	default:
		return "", fmt.Errorf("network mode must be lan or tailscale")
	}
}

func effectiveNetworkMode(config Config) NetworkMode {
	mode, err := parseNetworkMode(string(config.NetworkMode))
	if err != nil {
		return networkModeLAN
	}
	return mode
}

func tailscaleExecutable() (string, error) {
	if executable, err := exec.LookPath("tailscale"); err == nil {
		return executable, nil
	}
	const appExecutable = "/Applications/Tailscale.app/Contents/MacOS/Tailscale"
	if info, err := os.Stat(appExecutable); err == nil && !info.IsDir() {
		return appExecutable, nil
	}
	return "", errors.New("Tailscale is not installed; install and sign in to the Tailscale macOS app")
}

func detectTailnet() (tailnetStatus, error) {
	executable, err := tailscaleExecutable()
	if err != nil {
		return tailnetStatus{}, err
	}
	command := exec.Command(executable, "status", "--json")
	command.Env = append(os.Environ(), "TAILSCALE_BE_CLI=1")
	output, err := command.Output()
	if err != nil {
		return tailnetStatus{}, fmt.Errorf("read Tailscale status: %w", err)
	}
	return parseTailnetStatus(output)
}

func parseTailnetStatus(contents []byte) (tailnetStatus, error) {
	var document tailscaleStatusDocument
	if err := json.Unmarshal(contents, &document); err != nil {
		return tailnetStatus{}, fmt.Errorf("decode Tailscale status: %w", err)
	}
	if document.BackendState != "Running" {
		state := document.BackendState
		if state == "" {
			state = "unknown"
		}
		return tailnetStatus{}, fmt.Errorf("Tailscale is not connected (state: %s)", state)
	}
	ips := document.Self.TailscaleIPs
	if len(ips) == 0 {
		ips = document.TailscaleIPs
	}
	ipv4 := firstIPv4(ips)
	if ipv4 == "" {
		return tailnetStatus{}, errors.New("Tailscale is connected but has no IPv4 address")
	}
	dnsName := normalizeDNSName(document.Self.DNSName)
	status := tailnetStatus{
		IPv4:            ipv4,
		DNSName:         dnsName,
		MagicDNSName:    shortDNSName(dnsName),
		MagicDNSEnabled: document.CurrentTailnet.MagicDNSEnabled,
	}
	for _, peer := range document.Peer {
		peerDNS := normalizeDNSName(peer.DNSName)
		name := shortDNSName(peerDNS)
		if name == "" {
			name = strings.TrimSpace(peer.HostName)
		}
		status.Peers = append(status.Peers, tailnetPeer{
			ID: peer.ID, Name: name, DNSName: peerDNS, IPv4: firstIPv4(peer.TailscaleIPs),
			OS: peer.OS, Online: peer.Online, Active: peer.Active,
		})
	}
	sort.Slice(status.Peers, func(left, right int) bool { return status.Peers[left].Name < status.Peers[right].Name })
	return status, nil
}

func firstIPv4(values []string) string {
	for _, value := range values {
		parsed := net.ParseIP(strings.TrimSpace(value))
		if parsed != nil && parsed.To4() != nil {
			return parsed.String()
		}
	}
	return ""
}

func normalizeDNSName(value string) string {
	return strings.TrimSuffix(strings.TrimSpace(value), ".")
}

func shortDNSName(value string) string {
	name, _, _ := strings.Cut(normalizeDNSName(value), ".")
	return name
}

func parseTailnetAddress(value string) (string, error) {
	switch normalized := strings.ToLower(strings.TrimSpace(value)); normalized {
	case "", tailnetAddressMagicDNS:
		return tailnetAddressMagicDNS, nil
	case tailnetAddressIP:
		return tailnetAddressIP, nil
	default:
		return "", errors.New("Tailscale address must be magicdns or ip")
	}
}

func networkOrigin(mode NetworkMode, lan string, tailnet tailnetStatus, gatewayPort int, tailnetAddress string) (string, error) {
	switch mode {
	case networkModeLAN:
		if lan == "" {
			return "", errors.New("no active LAN IPv4 address found")
		}
		return "http://" + net.JoinHostPort(lan, fmt.Sprint(gatewayPort)), nil
	case networkModeTailscale:
		address, err := parseTailnetAddress(tailnetAddress)
		if err != nil {
			return "", err
		}
		host := tailnet.IPv4
		if address == tailnetAddressMagicDNS {
			if !tailnet.MagicDNSEnabled || tailnet.MagicDNSName == "" {
				return "", errors.New("Tailscale MagicDNS is not enabled; use --address ip")
			}
			host = tailnet.MagicDNSName
		}
		if host == "" {
			return "", errors.New("no active Tailscale IPv4 address found")
		}
		return "http://" + net.JoinHostPort(host, fmt.Sprint(gatewayPort)), nil
	default:
		return "", fmt.Errorf("unsupported network mode %q", mode)
	}
}

func onlineIOSPeers(tailnet tailnetStatus) []tailnetPeer {
	var peers []tailnetPeer
	for _, peer := range tailnet.Peers {
		if peer.Online && strings.EqualFold(peer.OS, "iOS") {
			peers = append(peers, peer)
		}
	}
	return peers
}

func chooseTailnetPeer(tailnet tailnetStatus, selector string, input io.Reader, output io.Writer, interactive bool) (tailnetPeer, error) {
	peers := onlineIOSPeers(tailnet)
	if len(peers) == 0 {
		return tailnetPeer{}, errors.New("no online iPhone or iPad found in this Tailnet")
	}
	selector = strings.TrimSpace(selector)
	if selector != "" {
		for _, peer := range peers {
			if peerMatches(peer, selector) {
				return peer, nil
			}
		}
		return tailnetPeer{}, fmt.Errorf("online iOS device %q was not found", selector)
	}
	if len(peers) == 1 {
		return peers[0], nil
	}
	if !interactive {
		return tailnetPeer{}, fmt.Errorf("multiple online iOS devices found; rerun with --device <%s>", joinedPeerNames(peers))
	}
	fmt.Fprintln(output, "Select the iPhone or iPad that will use this pairing grant:")
	for index, peer := range peers {
		fmt.Fprintf(output, "  %d. %s (%s)\n", index+1, peer.Name, peer.IPv4)
	}
	fmt.Fprint(output, "Device: ")
	line, err := bufio.NewReader(input).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return tailnetPeer{}, err
	}
	choice, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil || choice < 1 || choice > len(peers) {
		return tailnetPeer{}, errors.New("invalid device selection")
	}
	return peers[choice-1], nil
}

func peerMatches(peer tailnetPeer, selector string) bool {
	selector = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(selector)), ".")
	for _, candidate := range []string{peer.ID, peer.Name, peer.DNSName, peer.IPv4} {
		if strings.ToLower(candidate) == selector {
			return true
		}
	}
	return false
}

func joinedPeerNames(peers []tailnetPeer) string {
	names := make([]string, 0, len(peers))
	for _, peer := range peers {
		names = append(names, peer.Name)
	}
	return strings.Join(names, "|")
}
