package main

import (
	"context"
	"encoding/binary"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func encodedName(name string) []byte {
	out := []byte{}
	for _, label := range strings.Split(strings.TrimSuffix(name, "."), ".") {
		out = append(out, byte(len(label)))
		out = append(out, label...)
	}
	return append(out, 0)
}

func nameReply(kind string, data []byte) ([]byte, string) {
	packet, owner := nameQuery(0x1234, "192.168.1.24", kind)
	binary.BigEndian.PutUint16(packet[2:], 0x8400)
	binary.BigEndian.PutUint16(packet[6:], 1)
	typeID := byte(12)
	if kind == "netbios" {
		typeID = 0x21
	}
	packet = append(packet, 0xc0, 12, 0, typeID, 0, 1, 0, 0, 0, 10, byte(len(data)>>8), byte(len(data)))
	return append(packet, data...), owner
}

func TestNameQueriesAndPTRReplies(t *testing.T) {
	query, owner := nameQuery(0x1234, "192.168.1.24", "mdns")
	if owner != "24.1.168.192.in-addr.arpa" || binary.BigEndian.Uint16(query) != 0x1234 {
		t.Fatal(owner, query)
	}
	decoded, end, err := dnsName(query, 12)
	if err != nil || decoded != owner || string(query[end:]) != string([]byte{0, 12, 0, 1}) {
		t.Fatal(decoded, end, err)
	}
	packet, owner := nameReply("mdns", encodedName("工作站.local"))
	if got := parseNameAnswer(packet, 0x1234, owner, "mdns"); got != "工作站.local" {
		t.Fatal(got)
	}
	if got := parseNameAnswer(packet, 0xabcd, owner, "mdns"); got != "" {
		t.Fatal("accepted wrong transaction")
	}
	if got := parseNameAnswer(packet, 0x1234, "1.1.168.192.in-addr.arpa", "mdns"); got != "" {
		t.Fatal("accepted wrong IP owner")
	}
	for n := 0; n < len(packet); n++ {
		if got := parseNameAnswer(packet[:n], 0x1234, owner, "mdns"); got != "" {
			t.Fatal("accepted truncated reply", n)
		}
	}
	query, _ = nameQuery(1, "bad", "mdns")
	if query != nil {
		t.Fatal("accepted invalid query IP")
	}
	for _, name := range []string{"<script>.local", "192.168.1.24", "24.1.168.192.in-addr.arpa"} {
		p, o := nameReply("mdns", encodedName(name))
		if parseNameAnswer(p, 0x1234, o, "mdns") != "" {
			t.Fatal("accepted invalid PTR name", name)
		}
	}
}

func netbiosEntry(name string, suffix byte, flags uint16) []byte {
	entry := make([]byte, 18)
	for i := 0; i < 15; i++ {
		entry[i] = ' '
	}
	copy(entry[:15], name)
	entry[15] = suffix
	binary.BigEndian.PutUint16(entry[16:], flags)
	return entry
}

func TestNetBIOSNodeStatusFiltersGroupNames(t *testing.T) {
	query, owner := nameQuery(0x1234, "192.168.1.24", "netbios")
	if owner != "CK"+strings.Repeat("A", 30) || query[12] != 32 {
		t.Fatal(owner, query)
	}
	data := []byte{3}
	data = append(data, netbiosEntry("WORKGROUP", 0, 0x8000)...)
	data = append(data, netbiosEntry("SERVER", 0x20, 0)...)
	data = append(data, netbiosEntry("DESKTOP-TEST", 0, 0)...)
	packet, owner := nameReply("netbios", data)
	if name := parseNameAnswer(packet, 0x1234, owner, "netbios"); name != "DESKTOP-TEST" {
		t.Fatal(name)
	}
	packet, owner = nameReply("netbios", append([]byte{1}, netbiosEntry("NAS-SERVER", 0x20, 0)...))
	if name := parseNameAnswer(packet, 0x1234, owner, "netbios"); name != "NAS-SERVER" {
		t.Fatal(name)
	}
	packet, owner = nameReply("netbios", append([]byte{2}, netbiosEntry("DESKTOP-TEST", 0, 0)...))
	if name := parseNameAnswer(packet, 0x1234, owner, "netbios"); name != "" {
		t.Fatal("accepted truncated node table")
	}
}

func TestHostnameCleaningAndCompressionBounds(t *testing.T) {
	for input, want := range map[string]string{" Workstation.local. ": "Workstation.local", "周的电脑.local": "周的电脑.local", "NAS-SERVER": "NAS-SERVER", "Printer_1.lan": "Printer_1.lan"} {
		if got := cleanDeviceName(input); got != want {
			t.Fatal(input, got)
		}
	}
	for _, name := range []string{"", "...", "bad\x00name", "bad\nname", "<img src=x>", "192.168.1.24", "::1", "1.168.192.in-addr.arpa", strings.Repeat("a", 254), string([]byte{255})} {
		if cleanDeviceName(name) != "" {
			t.Fatal("accepted invalid hostname", name)
		}
	}
	for _, packet := range [][]byte{{0xc0, 0}, {0xc0, 2, 0xc0, 0}, {0xc0, 255}, {64}, {1}, {255}, {3, 'a', 'b'}} {
		if _, _, err := dnsName(packet, 0); err == nil {
			t.Fatal("accepted malformed DNS name", packet)
		}
	}
}

func TestNamesAreOptionalBoundedAndDoNotChangeAddresses(t *testing.T) {
	devices := make([]DiscoveredDevice, 64)
	for i := range devices {
		devices[i] = DiscoveredDevice{IP: fmt.Sprintf("192.168.1.%d", i+1), MAC: "02:11:22:33:44:55", Network: "eth0"}
	}
	var current, maxSeen atomic.Int32
	lookup := func(ctx context.Context, n Network, ip string) string {
		count := current.Add(1)
		defer current.Add(-1)
		for last := maxSeen.Load(); count > last && !maxSeen.CompareAndSwap(last, count); last = maxSeen.Load() {
		}
		if ip == "192.168.1.1" {
			return "Workstation.local."
		}
		<-ctx.Done()
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Millisecond)
	defer cancel()
	started := time.Now()
	nameDiscoveredDevices(ctx, Network{}, devices, lookup)
	if time.Since(started) > time.Second || maxSeen.Load() > 16 {
		t.Fatal("unbounded hostname lookup")
	}
	if devices[0].Name != "Workstation.local" {
		t.Fatal(devices[0])
	}
	for i, device := range devices {
		if device.IP != fmt.Sprintf("192.168.1.%d", i+1) || device.MAC != "02:11:22:33:44:55" || device.Network != "eth0" {
			t.Fatal("hostname lookup changed address")
		}
	}
	cancelled, stop := context.WithCancel(context.Background())
	stop()
	nameDiscoveredDevices(cancelled, Network{}, devices, func(context.Context, Network, string) string { t.Error("lookup after cancellation"); return "" })
}

func FuzzDiscoveryNameReply(f *testing.F) {
	packet, owner := nameReply("mdns", encodedName("Test-PC.local"))
	f.Add(packet)
	f.Add([]byte{0xc0, 0})
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, packet []byte) {
		_ = parseNameAnswer(packet, 0x1234, owner, "mdns")
		_ = parseNameAnswer(packet, 0x1234, "CK"+strings.Repeat("A", 30), "netbios")
		if len(packet) > 0 {
			_, _, _ = dnsName(packet, 0)
		}
	})
}
