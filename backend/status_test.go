package main

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestOptionalIPAndSystemPersistAndLegacyStillWakes(t *testing.T) {
	a := fixture(t)
	d := add(t, a)
	if d.IP != "" || d.OS != "" {
		t.Fatal(d)
	}
	for _, osType := range []string{"windows", "macos", "linux", "other", ""} {
		status, body := call(t, a, "PUT", "devices/"+d.ID, `{"name":"电脑","mac":"`+d.MAC+`","network":"auto","ip":" 192.168.13.20 ","os":"`+osType+`"}`, "admin")
		if status != 200 {
			t.Fatal(status, string(body))
		}
		loaded, err := newApp(filepath.Dir(a.file))
		if err != nil || loaded.state.Devices[0].IP != "192.168.13.20" || loaded.state.Devices[0].OS != osType {
			t.Fatal(loaded, err)
		}
	}
	for _, ip := range []string{"127.0.0.1", "0.0.0.0", "8.8.8.8", "169.254.1.2", "192.168.12.0", "192.168.13.255", testNetwork.IP, "localhost", "192.168.12.2:80", "::1", "192.168.012.1"} {
		status, body := call(t, a, "PUT", "devices/"+d.ID, `{"name":"电脑","mac":"`+d.MAC+`","network":"auto","ip":"`+ip+`"}`, "admin")
		if status != 400 {
			t.Fatal(ip, status, string(body))
		}
	}
	if status, _ := call(t, a, "PUT", "devices/"+d.ID, `{"name":"电脑","mac":"`+d.MAC+`","network":"auto","os":"guess"}`, "admin"); status != 400 {
		t.Fatal(status)
	}
	if status, _ := call(t, a, "PUT", "devices/"+d.ID, `{"name":"旧设备","mac":"`+d.MAC+`","network":"auto"}`, "admin"); status != 200 {
		t.Fatal(status)
	}
	if status, _ := call(t, a, "POST", "wake/"+d.ID, `{}`, "admin"); status != 200 {
		t.Fatal("optional fields must not affect WOL", status)
	}
}

func TestStatusRequiresAdminAndCachesOnlyMatchingConfiguration(t *testing.T) {
	a := fixture(t)
	d := add(t, a)
	calls := 0
	a.checkStatuses = func(ctx context.Context, devices []Device, networks []Network) []DeviceStatus {
		calls++
		if end, ok := ctx.Deadline(); !ok || time.Until(end) > 9*time.Second {
			t.Fatal("unbounded status request")
		}
		return []DeviceStatus{{ID: devices[0].ID, State: "unknown", CheckedAt: time.Now().UTC()}}
	}
	for _, tc := range []struct {
		who, body string
		want      int
	}{{"", `{}`, 401}, {"users", `{}`, 403}, {"admin", `{"ip":"8.8.8.8"}`, 400}} {
		if code, _ := call(t, a, "POST", "status", tc.body, tc.who); code != tc.want {
			t.Fatal(code, tc.want)
		}
	}
	if calls != 0 {
		t.Fatal("invalid request triggered probes")
	}
	before, _ := os.ReadFile(a.file)
	for i := 0; i < 2; i++ {
		if code, _ := call(t, a, "POST", "status", `{}`, "admin"); code != 200 {
			t.Fatal(code)
		}
	}
	if calls != 1 {
		t.Fatal("cache not used", calls)
	}
	after, _ := os.ReadFile(a.file)
	if string(before) != string(after) {
		t.Fatal("status changed persistent configuration")
	}
	if code, _ := call(t, a, "PUT", "devices/"+d.ID, `{"name":"修改","mac":"`+d.MAC+`","network":"auto","ip":"192.168.12.20"}`, "admin"); code != 200 {
		t.Fatal(code)
	}
	call(t, a, "POST", "status", `{}`, "admin")
	if calls != 2 {
		t.Fatal("stale config cache", calls)
	}
	if code, _ := call(t, a, "POST", "wake/"+d.ID, `{}`, "admin"); code != 200 {
		t.Fatal(code)
	}
	call(t, a, "POST", "status", `{}`, "admin")
	if calls != 3 {
		t.Fatal("wake did not invalidate status cache", calls)
	}
}

func TestStatusDoesNotBlockInfoAndReleasesAfterCompletion(t *testing.T) {
	a := fixture(t)
	add(t, a)
	started := make(chan struct{})
	release := make(chan struct{})
	a.checkStatuses = func(ctx context.Context, _ []Device, _ []Network) []DeviceStatus {
		close(started)
		select {
		case <-ctx.Done():
		case <-release:
		}
		return []DeviceStatus{}
	}
	done := make(chan struct{})
	go func() { call(t, a, "POST", "status", `{}`, "admin"); close(done) }()
	<-started
	if code, _ := call(t, a, "POST", "status", `{}`, "admin"); code != 409 {
		t.Fatal(code)
	}
	if code, _ := call(t, a, "GET", "info", "", "admin"); code != 200 {
		t.Fatal(code)
	}
	close(release)
	<-done
	if a.statusBusy {
		t.Fatal("status lock retained")
	}
}

func TestStatusEvidenceAndMACMatching(t *testing.T) {
	d := Device{ID: "a", MAC: "02:11:22:33:44:55", Network: "auto"}
	entry := DiscoveredDevice{IP: "192.168.12.20", MAC: d.MAC, Network: testNetwork.ID}
	for _, tc := range []struct {
		name     string
		entry    bool
		response bool
		wrongMAC bool
		arpError bool
		explicit bool
		want     string
	}{
		{"no address", false, false, false, false, false, "unknown"},
		{"cached ARP is not online", true, false, false, false, false, "unresponsive"},
		{"response and matching MAC", true, true, false, false, false, "online"},
		{"explicit IP through MAC-translating relay", true, true, true, false, true, "online"},
		{"explicit IP without ARP access", false, true, false, true, true, "online"},
		{"inferred IP now belongs to another MAC", true, true, true, false, false, "unknown"},
		{"inferred IP cannot verify MAC", true, true, false, true, false, "unknown"},
		{"explicit IP no response despite MAC mismatch", true, false, true, false, true, "unresponsive"},
		{"manual IP works", true, true, false, false, true, "online"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			device := d
			if tc.explicit {
				device.IP = entry.IP
			}
			entries := []DiscoveredDevice{}
			if tc.entry {
				entries = append(entries, entry)
			}
			deps := statusDependencies{
				neighbours: func(Network, netip.Prefix) ([]DiscoveredDevice, error) {
					if tc.arpError {
						return nil, errors.New("denied")
					}
					copy := append([]DiscoveredDevice{}, entries...)
					if tc.wrongMAC {
						copy[0].MAC = "02:AA:AA:AA:AA:AA"
					}
					return copy, nil
				},
				probe: func(context.Context, Network, string) (string, error) {
					if tc.response {
						return "TCP", nil
					}
					return "", errors.New("timeout")
				},
			}
			result := checkOneStatus(context.Background(), device, []Network{testNetwork}, map[string][]DiscoveredDevice{testNetwork.ID: entries}, deps)
			if result.State != tc.want || result.ID != d.ID || result.CheckedAt.IsZero() {
				t.Fatal(result)
			}
			if tc.explicit && result.Source != "ip" {
				t.Fatal("explicit IP lost its source", result)
			}
			if tc.explicit && tc.response && (tc.wrongMAC || tc.arpError) && result.MACVerified {
				t.Fatal("unverified MAC was confirmed", result)
			}
		})
	}
}

func TestExplicitIPDoesNotScanOrFallBackToAnotherAddress(t *testing.T) {
	d := Device{ID: "a", MAC: "02:11:22:33:44:55", IP: "192.168.12.121", Network: "auto"}
	entry := DiscoveredDevice{IP: "192.168.12.74", MAC: d.MAC, Network: testNetwork.ID}
	probes := []string{}
	deps := statusDependencies{
		neighbours: func(Network, netip.Prefix) ([]DiscoveredDevice, error) { return []DiscoveredDevice{entry}, nil },
		discover: func(context.Context, Network) (ScanResult, error) {
			t.Error("explicit IP triggered discovery")
			return ScanResult{}, nil
		},
		probe: func(_ context.Context, _ Network, ip string) (string, error) {
			probes = append(probes, ip)
			if ip != d.IP {
				return "TCP", nil
			}
			return "", errors.New("no response")
		},
	}
	results := checkStatusesWith(context.Background(), []Device{d}, []Network{testNetwork}, deps)
	if results[0].State != "unresponsive" || results[0].IP != d.IP || len(probes) != 1 || probes[0] != d.IP {
		t.Fatal(results, probes)
	}
}

func TestStatusDiscoverySharedAndTargetsBounded(t *testing.T) {
	var discoveries, probes atomic.Int32
	devices := []Device{{ID: "a", MAC: "02:11:22:33:44:55", Network: "auto"}, {ID: "b", MAC: "02:11:22:33:44:66", Network: "auto"}}
	entries := []DiscoveredDevice{{IP: "192.168.12.20", MAC: devices[0].MAC, Network: testNetwork.ID}, {IP: "192.168.12.21", MAC: devices[1].MAC, Network: testNetwork.ID}}
	deps := statusDependencies{
		neighbours: func(Network, netip.Prefix) ([]DiscoveredDevice, error) {
			if discoveries.Load() > 0 {
				return entries, nil
			}
			return nil, nil
		},
		discover: func(ctx context.Context, n Network) (ScanResult, error) {
			discoveries.Add(1)
			return ScanResult{Devices: entries}, nil
		},
		probe: func(ctx context.Context, n Network, ip string) (string, error) {
			if _, ok := statusNetwork(ip, n.ID, []Network{testNetwork}); !ok {
				t.Error("out of scope", ip)
			}
			probes.Add(1)
			return "TCP", nil
		},
	}
	got := checkStatusesWith(context.Background(), devices, []Network{testNetwork}, deps)
	if discoveries.Load() != 1 || probes.Load() != 2 || got[0].State != "online" || got[1].State != "online" {
		t.Fatal(got, discoveries.Load(), probes.Load())
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got = checkStatusesWith(ctx, devices, nil, deps)
	if len(got) != 2 {
		t.Fatal(got)
	}
}

func TestStatusTriesDifferentIPsWithTheSameMAC(t *testing.T) {
	d := Device{ID: "same-mac", MAC: "02:11:22:33:44:55", Network: "auto"}
	entries := []DiscoveredDevice{
		{IP: "192.168.12.74", MAC: d.MAC, Network: testNetwork.ID},
		{IP: "192.168.12.121", MAC: d.MAC, Network: testNetwork.ID},
	}
	probes := []string{}
	deps := statusDependencies{
		neighbours: func(Network, netip.Prefix) ([]DiscoveredDevice, error) { return entries, nil },
		probe: func(_ context.Context, _ Network, ip string) (string, error) {
			probes = append(probes, ip)
			if ip == entries[1].IP {
				return "TCP", nil
			}
			return "", errors.New("no response")
		},
	}
	result := checkOneStatus(context.Background(), d, []Network{testNetwork}, map[string][]DiscoveredDevice{testNetwork.ID: entries}, deps)
	if result.State != "online" || result.IP != entries[1].IP || len(probes) != 2 {
		t.Fatal(result, probes)
	}
}

func TestTCPStatusProbeOnWireAndCancellation(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if !tcpStatusProbe(ctx, "127.0.0.1", "127.0.0.1", port) {
		t.Fatal("open TCP not detected")
	}
	listener.Close()
	if !tcpStatusProbe(ctx, "127.0.0.1", "127.0.0.1", port) {
		t.Fatal("refusal is also a response")
	}
	cancel()
	if tcpStatusProbe(ctx, "127.0.0.1", "127.0.0.1", port) {
		t.Fatal("cancelled probe succeeded")
	}
}

func TestStatusOnNAS(t *testing.T) {
	if os.Getenv("WOL_STATUS_INTEGRATION") != "1" {
		t.Skip("real NAS detection is opt-in")
	}
	networks, err := localNetworks()
	if err != nil || len(networks) == 0 {
		t.Fatal(err)
	}
	n := networks[0]
	prefix, _ := netip.ParsePrefix(n.CIDR)
	entries, err := arpSnapshot(n, prefix.Masked())
	if err != nil {
		t.Fatal(err)
	}
	devices := []Device{}
	for _, entry := range entries {
		if len(devices) == 8 {
			break
		}
		devices = append(devices, Device{ID: entry.MAC, MAC: entry.MAC, IP: entry.IP, Network: n.ID})
	}
	if len(devices) == 0 {
		t.Fatal("no neighbours available")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 9*time.Second)
	defer cancel()
	results := checkDeviceStatuses(ctx, devices, networks)
	counts := map[string]int{}
	for _, result := range results {
		counts[result.State]++
	}
	data, _ := json.Marshal(counts)
	t.Logf("Unprivileged NAS status detection: %d devices, %s", len(results), data)
	if counts["online"] == 0 {
		t.Fatal("expected at least one responsive network peer")
	}
	for _, r := range results {
		if strings.Contains(r.Reason, "<") {
			t.Fatal(r)
		}
	}
}
