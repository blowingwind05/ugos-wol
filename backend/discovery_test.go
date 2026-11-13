package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"net/netip"
	"os"
	"strings"
	"testing"
	"time"
)

func TestDiscoveryRangesAndTargets(t *testing.T) {
	for _, tc := range []struct {
		cidr, ip, want string
		count          int
		limited        bool
	}{
		{"192.168.12.0/23", "192.168.12.10", "192.168.12.0/24", 253, true},
		{"10.20.0.0/16", "10.20.42.8", "10.20.42.0/24", 253, true},
		{"10.20.42.0/24", "10.20.42.8", "10.20.42.0/24", 253, false},
		{"10.20.42.0/30", "10.20.42.1", "10.20.42.0/30", 1, false},
	} {
		prefix, limited, err := discoveryRange(Network{IP: tc.ip, CIDR: tc.cidr})
		if err != nil || prefix.String() != tc.want || limited != tc.limited {
			t.Fatalf("%s %t %v", prefix, limited, err)
		}
		targets := discoveryTargets(prefix, tc.ip)
		if len(targets) != tc.count {
			t.Fatal(len(targets))
		}
		for _, ip := range targets {
			if !prefix.Contains(ip) || !prefix.Contains(ip.Next()) || ip == prefix.Addr() || ip.String() == tc.ip {
				t.Fatalf("invalid target %s", ip)
			}
		}
	}
	for _, n := range []Network{
		{IP: "10.0.0.2", CIDR: "bad"}, {IP: "bad", CIDR: "10.0.0.0/24"},
		{IP: "10.0.1.2", CIDR: "10.0.0.0/24"}, {IP: "10.0.0.2", CIDR: "10.0.0.2/31"},
		{IP: "10.0.0.2", CIDR: "10.0.0.2/32"}, {IP: "::1", CIDR: "::/64"},
		{IP: "127.0.0.1", CIDR: "127.0.0.0/24"}, {IP: "169.254.1.2", CIDR: "169.254.1.0/24"},
	} {
		if _, _, err := discoveryRange(n); err == nil {
			t.Errorf("accepted invalid interface %+v", n)
		}
	}
}

func TestDiscoveryARPFiltersAndDeduplication(t *testing.T) {
	n := Network{ID: "eth0/10.0.0.0/24", Name: "eth0", IP: "10.0.0.2", CIDR: "10.0.0.0/24"}
	cache := `IP address HW type Flags HW address Mask Device
10.0.0.120 0x1 0x2 02:11:22:33:44:55 * eth0
10.0.0.9 0x1 0x6 02:11:22:33:44:66 * eth0
10.0.0.7 0x1 0x2 02:aa:bb:cc:dd:ee * eth0
10.0.0.8 0x1 0x2 02:aa:bb:cc:dd:ee * eth0
10.0.0.8 0x1 0x2 02:AA:BB:CC:DD:EE * eth0
10.0.0.2 0x1 0x2 02:10:20:30:40:50 * eth0
10.0.0.10 0x1 0x0 02:10:20:30:40:51 * eth0
10.0.0.11 0x1 0x2 00:00:00:00:00:00 * eth0
10.0.0.12 0x1 0x2 FF:FF:FF:FF:FF:FF * eth0
10.0.0.13 0x1 0x2 01:11:22:33:44:55 * eth0
10.0.0.14 0x1 0x2 invalid * eth0
10.0.0.15 0x1 0x2 02:10:20:30:40:52 * docker0
10.0.1.16 0x1 0x2 02:10:20:30:40:53 * eth0
10.0.0.17 broken 0x2 02:10:20:30:40:54 * eth0
10.0.0.18 0x1 broken 02:10:20:30:40:55 * eth0
10.0.0.19 0x2 0x2 02:10:20:30:40:56 * eth0
10.0.0.255 0x1 0x2 02:10:20:30:40:57 * eth0
10.0.0.0 0x1 0x2 02:10:20:30:40:58 * eth0
not-an-ip 0x1 0x2 02:10:20:30:40:59 * eth0
incomplete row
`
	got, err := readARP(strings.NewReader(cache), n, netip.MustParsePrefix(n.CIDR))
	if err != nil || len(got) != 4 || got[0].IP != "10.0.0.7" || got[0].MAC != "02:AA:BB:CC:DD:EE" || got[1].IP != "10.0.0.8" || got[1].MAC != got[0].MAC || got[2].IP != "10.0.0.9" || got[3].IP != "10.0.0.120" {
		t.Fatal(got, err)
	}
	for _, d := range got {
		if d.Network != n.ID {
			t.Fatal(d)
		}
	}
	if empty, err := readARP(strings.NewReader(""), n, netip.MustParsePrefix(n.CIDR)); err != nil || empty == nil || len(empty) != 0 {
		t.Fatal(empty, err)
	}
}

func TestDiscoveryRequiresAdminAndCurrentInterface(t *testing.T) {
	a := fixture(t)
	calls := 0
	a.discover = func(ctx context.Context, n Network) (ScanResult, error) {
		calls++
		if n != testNetwork {
			t.Fatal(n)
		}
		if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) > 10*time.Second {
			t.Fatal("unbounded scan context")
		}
		return ScanResult{Devices: []DiscoveredDevice{}, CIDR: "192.168.12.0/24", Limited: true}, nil
	}
	valid := `{"network":"` + testNetwork.ID + `"}`
	for _, tc := range []struct {
		identity, body string
		status         int
	}{
		{"", valid, 401}, {"users", valid, 403}, {"admin", `{"network":"8.8.8.8"}`, 400},
		{"admin", `{"network":"auto"}`, 400}, {"admin", `{"network":"eth0;touch /tmp/unwanted"}`, 400},
		{"admin", `{"network":"` + testNetwork.ID + `","cidr":"8.8.8.0/24"}`, 400}, {"admin", `{} {}`, 400},
	} {
		if status, body := call(t, a, "POST", "scan", tc.body, tc.identity); status != tc.status {
			t.Fatal(status, string(body))
		}
	}
	if calls != 0 {
		t.Fatal("unauthorized or arbitrary target discovery ran")
	}
	before, _ := json.Marshal(a.state)
	if status, body := call(t, a, "POST", "scan", valid, "admin"); status != 200 || !strings.Contains(string(body), `"devices":[]`) {
		t.Fatal(status, string(body))
	}
	if status, _ := call(t, a, "POST", "scan", valid, "admin"); status != 429 || calls != 1 {
		t.Fatal(status, calls)
	}
	after, _ := json.Marshal(a.state)
	if string(before) != string(after) {
		t.Fatal("scan changed saved state")
	}
}

func TestDiscoveryConcurrencyCancellationAndRecovery(t *testing.T) {
	a := fixture(t)
	started := make(chan struct{})
	a.discover = func(ctx context.Context, n Network) (ScanResult, error) {
		close(started)
		<-ctx.Done()
		return ScanResult{}, ctx.Err()
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	body := `{"network":"` + testNetwork.ID + `"}`
	r := httptest.NewRequest("POST", api+"scan", strings.NewReader(body)).WithContext(ctx)
	r.RemoteAddr = "127.0.0.1:1234"
	for k, v := range map[string]string{"Content-Type": "application/json", "Ugreen-User-ID": "100", "Ugreen-User-Name": "测试管理员", "Ugreen-User-Type": "admin"} {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	done := make(chan struct{})
	go func() { a.handler().ServeHTTP(w, r); close(done) }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("scan did not start")
	}
	if status, _ := call(t, a, "POST", "scan", body, "admin"); status != 409 {
		t.Fatal(status)
	}
	if status, _ := call(t, a, "GET", "info", "", "admin"); status != 200 {
		t.Fatal("scan blocked info", status)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("scan ignored cancellation")
	}
	if w.Code != 504 || a.scanning {
		t.Fatal(w.Code, a.scanning)
	}
	a.lastScan = time.Time{}
	a.discover = func(context.Context, Network) (ScanResult, error) {
		return ScanResult{}, errors.New("测试读取失败")
	}
	if status, body := call(t, a, "POST", "scan", body, "admin"); status != 503 || !strings.Contains(string(body), "测试读取失败") {
		t.Fatal(status, string(body))
	}
	if a.scanning {
		t.Fatal("failed scan kept lock")
	}
}

// Normal tests never probe the LAN. This explicit diagnostic runs only on the
// real NAS, with the same unprivileged identity as the app.
func TestDiscoveryOnNAS(t *testing.T) {
	if os.Getenv("WOL_SCAN_INTEGRATION") != "1" {
		t.Skip("real NAS discovery is opt-in")
	}
	networks, err := localNetworks()
	if err != nil || len(networks) == 0 {
		t.Fatal("no LAN interface", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	result, err := discoverLAN(ctx, networks[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Devices) == 0 {
		t.Fatal("expected at least one NAS LAN neighbour")
	}
	prefix := netip.MustParsePrefix(result.CIDR)
	for _, d := range result.Devices {
		if !prefix.Contains(netip.MustParseAddr(d.IP)) || d.Network != networks[0].ID {
			t.Fatal("result escaped selected interface")
		}
		if _, _, err := magicPacket(d.MAC); err != nil {
			t.Fatal("invalid MAC", err)
		}
	}
	named := 0
	expectedIP := os.Getenv("WOL_SCAN_EXPECT_IP")
	foundExpected := expectedIP == ""
	for _, device := range result.Devices {
		if device.IP == expectedIP {
			foundExpected = true
		}
		if device.Name != "" {
			named++
			if cleanDeviceName(device.Name) != device.Name {
				t.Fatal("invalid discovered hostname")
			}
		}
	}
	if !foundExpected {
		t.Fatal("expected target IP was absent from discovery results")
	}
	t.Logf("Unprivileged NAS discovery passed: %d neighbours, %d hostnames, bounded scope", len(result.Devices), named)
}
