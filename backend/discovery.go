package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"os"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const scanCooldown = 10 * time.Second

// Discovery is deliberately limited to a NAS interface, never an arbitrary
// client-supplied destination. Large LANs are sampled in the NAS's own /24.
type DiscoveredDevice struct {
	Name    string `json:"name,omitempty"`
	IP      string `json:"ip"`
	MAC     string `json:"mac"`
	Network string `json:"network"`
}

type ScanResult struct {
	Devices []DiscoveredDevice `json:"devices"`
	CIDR    string             `json:"cidr"`
	Limited bool               `json:"limited"`
}

func discoveryRange(network Network) (netip.Prefix, bool, error) {
	prefix, err := netip.ParsePrefix(network.CIDR)
	ip, ipErr := netip.ParseAddr(network.IP)
	if err != nil || ipErr != nil || !prefix.Addr().Is4() || !ip.Is4() || !ip.IsGlobalUnicast() || ip.IsLinkLocalUnicast() || !prefix.Contains(ip) || prefix.Bits() > 30 {
		return netip.Prefix{}, false, errors.New("所选接口没有可扫描的 IPv4 局域网")
	}
	limited := prefix.Bits() < 24
	if limited {
		prefix = netip.PrefixFrom(ip, 24)
	}
	return prefix.Masked(), limited, nil
}

func discoveryTargets(prefix netip.Prefix, ownIP string) []netip.Addr {
	addresses := []netip.Addr{}
	// Prefixes have already been validated and narrowed to at most /24.
	for ip := prefix.Addr().Next(); prefix.Contains(ip) && prefix.Contains(ip.Next()); ip = ip.Next() {
		if ip.String() != ownIP {
			addresses = append(addresses, ip)
		}
	}
	return addresses
}

func readARP(r io.Reader, network Network, prefix netip.Prefix) ([]DiscoveredDevice, error) {
	devices := []DiscoveredDevice{}
	scanner := bufio.NewScanner(io.LimitReader(r, 1<<20))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) != 6 || fields[5] != network.Name {
			continue
		}
		kind, kindErr := strconv.ParseUint(fields[1], 0, 32)
		flags, flagsErr := strconv.ParseUint(fields[2], 0, 32)
		ip, ipErr := netip.ParseAddr(fields[0])
		if kindErr != nil || flagsErr != nil || kind != 1 || flags&2 == 0 || ipErr != nil || !ip.Is4() || !prefix.Contains(ip) || ip.String() == network.IP {
			continue
		}
		// Skip subnet and broadcast addresses, including malformed cache data.
		if ip == prefix.Addr() || !prefix.Contains(ip.Next()) {
			continue
		}
		_, mac, err := magicPacket(fields[3])
		if err == nil {
			devices = append(devices, DiscoveredDevice{IP: ip.String(), MAC: mac, Network: network.ID})
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	sort.Slice(devices, func(i, j int) bool {
		return netip.MustParseAddr(devices[i].IP).Less(netip.MustParseAddr(devices[j].IP))
	})
	seen := map[string]bool{}
	unique := devices[:0]
	for _, device := range devices {
		// A neighbour can have multiple IPv4 addresses, and old addresses can
		// coexist with new ones. Do not drop candidates merely for sharing a MAC.
		key := device.IP + "/" + device.MAC
		if !seen[key] {
			unique = append(unique, device)
			seen[key] = true
		}
	}
	return unique, nil
}

func arpSnapshot(network Network, prefix netip.Prefix) ([]DiscoveredDevice, error) {
	// UGOS may expose /proc/self through a read-only bind instead of all /proc.
	var lastErr error
	for _, path := range []string{"/proc/net/arp", "/proc/self/net/arp"} {
		file, err := os.Open(path)
		if err != nil {
			lastErr = err
			continue
		}
		devices, err := readARP(file, network, prefix)
		file.Close()
		if err == nil {
			return devices, nil
		}
		lastErr = err
	}
	return nil, lastErr
}

func discoverNeighbours(ctx context.Context, network Network) (ScanResult, error) {
	prefix, limited, err := discoveryRange(network)
	result := ScanResult{Devices: []DiscoveredDevice{}, CIDR: prefix.String(), Limited: limited}
	if err != nil {
		return result, err
	}
	// Check cache access before probing. No raw sockets, subprocesses, root,
	// capability changes, magic packets, or scans of service ports are needed.
	if _, err := arpSnapshot(network, prefix); err != nil {
		return result, errors.New("无法读取 NAS 的局域网邻居信息，请手动填写 MAC 地址")
	}
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP(network.IP)})
	if err != nil {
		return result, errors.New("无法使用所选网络接口，请检查 NAS 网络连接")
	}
	defer conn.Close()
	stopClose := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stopClose()
	deadline := time.Now().Add(8 * time.Second)
	if end, ok := ctx.Deadline(); ok && end.Before(deadline) {
		deadline = end
	}
	if err := conn.SetWriteDeadline(deadline); err != nil {
		return result, err
	}
	pace := time.NewTicker(4 * time.Millisecond)
	defer pace.Stop()
	for _, ip := range discoveryTargets(prefix, network.IP) {
		select {
		case <-ctx.Done():
			return result, ctx.Err()
		case <-pace.C:
		}
		// A single non-magic UDP datagram prompts normal kernel ARP resolution.
		// Existing neighbour records can survive sleep or shutdown; the UI never
		// treats this result as proof of liveness or WOL support.
		if _, err := conn.WriteToUDP([]byte{0}, &net.UDPAddr{IP: net.IP(ip.AsSlice()), Port: 9}); err != nil {
			// An offline host or a closed discard port is normal during discovery.
			if errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, syscall.EHOSTUNREACH) || errors.Is(err, syscall.ENETUNREACH) {
				continue
			}
			return result, errors.New("局域网探测发送失败，请检查所选网络接口")
		}
	}
	settle := time.NewTimer(1500 * time.Millisecond)
	defer settle.Stop()
	select {
	case <-ctx.Done():
		return result, ctx.Err()
	case <-settle.C:
	}
	devices, err := arpSnapshot(network, prefix)
	if err != nil {
		return result, errors.New("无法读取扫描结果，请重试或手动填写 MAC 地址")
	}
	result.Devices = devices
	return result, nil
}

func discoverLAN(ctx context.Context, network Network) (ScanResult, error) {
	result, err := discoverNeighbours(ctx, network)
	if err == nil {
		nameDiscoveredDevices(ctx, network, result.Devices, lookupDeviceName)
	}
	return result, err
}

func (a *App) scanLAN(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Network string `json:"network"`
	}
	if !decode(w, r, &input) {
		return
	}
	networks, err := a.networks()
	if err != nil {
		problem(w, 500, "无法读取 NAS 网络接口")
		return
	}
	var selected *Network
	for i := range networks {
		if networks[i].ID == input.Network {
			selected = &networks[i]
			break
		}
	}
	if selected == nil {
		problem(w, 400, "请选择当前可用的 NAS 局域网接口")
		return
	}
	if _, _, err := discoveryRange(*selected); err != nil {
		problem(w, 400, err.Error())
		return
	}
	a.scanMu.Lock()
	if a.scanning {
		a.scanMu.Unlock()
		problem(w, 409, "已有局域网扫描正在进行，请稍后再试")
		return
	}
	if wait := scanCooldown - time.Since(a.lastScan); wait > 0 {
		a.scanMu.Unlock()
		seconds := int(wait.Seconds()) + 1
		w.Header().Set("Retry-After", strconv.Itoa(seconds))
		problem(w, 429, fmt.Sprintf("扫描过于频繁，请等待 %d 秒再试", seconds))
		return
	}
	a.scanning, a.lastScan = true, time.Now()
	a.scanMu.Unlock()
	defer func() {
		a.scanMu.Lock()
		a.scanning = false
		a.scanMu.Unlock()
	}()
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	result, err := a.discover(ctx, *selected)
	if err != nil {
		if ctx.Err() != nil {
			problem(w, 504, "扫描已取消或超时，请重试或手动填写 MAC 地址")
		} else {
			problem(w, 503, err.Error())
		}
		return
	}
	respond(w, 200, result)
}
