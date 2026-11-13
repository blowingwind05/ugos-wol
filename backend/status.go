package main

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const statusCacheTTL = 5 * time.Second

type DeviceStatus struct {
	ID          string    `json:"id"`
	State       string    `json:"state"`
	IP          string    `json:"ip,omitempty"`
	Method      string    `json:"method,omitempty"`
	Source      string    `json:"source,omitempty"`
	MACVerified bool      `json:"mac_verified"`
	Reason      string    `json:"reason"`
	CheckedAt   time.Time `json:"checked_at"`
}

// Only directly attached IPv4 device addresses are eligible. Status never
// follows user-supplied URLs, DNS names, routes, or arbitrary service ports.
func statusNetwork(value, selected string, networks []Network) (Network, bool) {
	ip, err := netip.ParseAddr(value)
	if err != nil || !ip.Is4() || !ip.IsGlobalUnicast() || ip.IsLinkLocalUnicast() {
		return Network{}, false
	}
	for _, n := range networks {
		if n.IP == value { // Never mistake this NAS for a target on another NIC.
			return Network{}, false
		}
	}
	for _, n := range networks {
		prefix, err := netip.ParsePrefix(n.CIDR)
		if err != nil || !prefix.Addr().Is4() || prefix.Bits() > 30 || (selected != "auto" && selected != n.ID) {
			continue
		}
		prefix = prefix.Masked()
		if prefix.Contains(ip) && ip != prefix.Addr() && prefix.Contains(ip.Next()) {
			return n, true
		}
	}
	return Network{}, false
}

func (a *App) deviceStatuses(w http.ResponseWriter, r *http.Request) {
	var input struct{}
	if !decode(w, r, &input) {
		return
	}
	networks, err := a.networks()
	if err != nil {
		problem(w, 503, "无法读取 NAS 网络接口")
		return
	}
	a.mu.Lock()
	devices := append([]Device{}, a.state.Devices...)
	a.mu.Unlock()
	key, _ := json.Marshal(struct {
		Devices  []Device
		Networks []Network
	}{devices, networks})
	a.statusMu.Lock()
	if a.statusCacheKey == string(key) && time.Since(a.statusCachedAt) < statusCacheTTL {
		cached := a.statusCache
		a.statusMu.Unlock()
		respond(w, 200, map[string]any{"devices": cached})
		return
	}
	if a.statusBusy {
		a.statusMu.Unlock()
		w.Header().Set("Retry-After", "3")
		problem(w, 409, "设备状态检测正在进行，请稍后再试")
		return
	}
	a.statusBusy = true
	a.statusMu.Unlock()
	defer func() { a.statusMu.Lock(); a.statusBusy = false; a.statusMu.Unlock() }()
	ctx, cancel := context.WithTimeout(r.Context(), 9*time.Second)
	defer cancel()
	result := a.checkStatuses(ctx, devices, networks)
	if r.Context().Err() != nil {
		return
	}
	a.statusMu.Lock()
	a.statusCacheKey, a.statusCache, a.statusCachedAt = string(key), result, time.Now()
	a.statusMu.Unlock()
	respond(w, 200, map[string]any{"devices": result})
}

type statusDependencies struct {
	neighbours func(Network, netip.Prefix) ([]DiscoveredDevice, error)
	discover   func(context.Context, Network) (ScanResult, error)
	probe      func(context.Context, Network, string) (string, error)
}

func checkDeviceStatuses(ctx context.Context, devices []Device, networks []Network) []DeviceStatus {
	return checkStatusesWith(ctx, devices, networks, statusDependencies{arpSnapshot, discoverNeighbours, probeStatus})
}

func checkStatusesWith(ctx context.Context, devices []Device, networks []Network, deps statusDependencies) []DeviceStatus {
	results := make([]DeviceStatus, len(devices))
	// Keep cached addresses as candidates, never as proof of being online.
	neighbours := map[string][]DiscoveredDevice{}
	needDiscovery := map[string]bool{}
	for _, n := range networks {
		prefix, err := netip.ParsePrefix(n.CIDR)
		if err != nil {
			continue
		}
		entries, _ := deps.neighbours(n, prefix.Masked())
		neighbours[n.ID] = entries
	}
	for _, d := range devices {
		if d.IP != "" {
			continue
		}
		for _, n := range networks {
			if d.Network != "auto" && d.Network != n.ID {
				continue
			}
			found := false
			for _, entry := range neighbours[n.ID] {
				if strings.EqualFold(entry.MAC, d.MAC) {
					found = true
					break
				}
			}
			if !found {
				needDiscovery[n.ID] = true
			}
		}
	}
	var mu sync.Mutex
	var wg sync.WaitGroup
	slots := make(chan struct{}, 4)
	// One bounded /24 neighbour refresh per needed interface, shared by devices.
	for _, n := range networks {
		if !needDiscovery[n.ID] {
			continue
		}
		wg.Add(1)
		go func(n Network) {
			defer wg.Done()
			select {
			case slots <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-slots }()
			discoveryCtx, cancel := context.WithTimeout(ctx, 3500*time.Millisecond)
			defer cancel()
			if result, err := deps.discover(discoveryCtx, n); err == nil {
				mu.Lock()
				neighbours[n.ID] = result.Devices
				mu.Unlock()
			}
		}(n)
	}
	wg.Wait()
	slots = make(chan struct{}, 8)
	for i, d := range devices {
		wg.Add(1)
		go func(i int, d Device) {
			defer wg.Done()
			select {
			case slots <- struct{}{}:
			case <-ctx.Done():
				results[i] = DeviceStatus{ID: d.ID, State: "unknown", Reason: "检测超时，请稍后重试", CheckedAt: time.Now().UTC()}
				return
			}
			defer func() { <-slots }()
			results[i] = checkOneStatus(ctx, d, networks, neighbours, deps)
		}(i, d)
	}
	wg.Wait()
	return results
}

func checkOneStatus(ctx context.Context, d Device, networks []Network, neighbours map[string][]DiscoveredDevice, deps statusDependencies) DeviceStatus {
	result := DeviceStatus{ID: d.ID, State: "unknown", Reason: "未找到对应 IP，可编辑设备选填 IP；自动查找最多覆盖每个接口所在的 /24", CheckedAt: time.Now().UTC()}
	candidates := []DiscoveredDevice{}
	if d.IP != "" {
		n, ok := statusNetwork(d.IP, d.Network, networks)
		if !ok {
			result.Reason = "所填 IP 不在当前所选 NAS 局域网内，请编辑设备核对"
			return result
		}
		candidates = append(candidates, DiscoveredDevice{IP: d.IP, MAC: d.MAC, Network: n.ID})
	}
	for _, n := range networks {
		if d.IP != "" {
			break
		} // Explicit IP is authoritative for reachability.
		if d.Network != "auto" && d.Network != n.ID {
			continue
		}
		for _, entry := range neighbours[n.ID] {
			if strings.EqualFold(entry.MAC, d.MAC) && entry.IP != d.IP {
				candidates = append(candidates, entry)
			}
		}
	}
	for _, candidate := range candidates {
		n, ok := statusNetwork(candidate.IP, candidate.Network, networks)
		if !ok {
			continue
		}
		probeCtx, cancel := context.WithTimeout(ctx, 1100*time.Millisecond)
		method, err := deps.probe(probeCtx, n, candidate.IP)
		cancel()
		result.IP = candidate.IP
		// The saved MAC identifies the WOL receiver. A user-selected IP is an
		// independent reachability target: relays can replace its observed MAC.
		// Automatically inferred IPs still require a matching MAC after probing.
		prefix, _ := netip.ParsePrefix(n.CIDR)
		entries, arpErr := deps.neighbours(n, prefix.Masked())
		matched, conflict := false, false
		for _, entry := range entries {
			if entry.IP == candidate.IP {
				matched = strings.EqualFold(entry.MAC, d.MAC)
				conflict = !matched
				break
			}
		}
		if d.IP != "" {
			result.Source = "ip"
			result.MACVerified = arpErr == nil && matched
			if err == nil {
				result.State, result.Method = "online", method
				result.Reason = "所填 IP 有网络响应；按 IP 检测，不代表远程服务已就绪"
				if !result.MACVerified {
					result.Reason += "。未核对到相同 MAC，可能经过无线中继或地址转换，请确认 IP 属于目标设备"
				}
			} else if ctx.Err() != nil {
				result.State, result.Reason = "unknown", "检测超时，请稍后重试"
			} else {
				result.State, result.Reason = "unresponsive", "所填 IP 未收到探测响应：可能关机、休眠，或被防火墙拦截"
			}
			result.CheckedAt = time.Now().UTC()
			return result
		}
		if conflict {
			result.State, result.Reason = "unknown", "该 IP 当前对应其他 MAC，请核对或清空 IP 后自动查找"
			continue
		}
		if err == nil {
			if arpErr != nil || !matched {
				result.State, result.Reason = "unknown", "地址有响应，但无法确认是此 MAC 对应的设备"
				continue
			}
			result.State, result.Method, result.Reason = "online", method, "设备网络有响应，且 MAC 匹配；不代表已登录或远程服务已就绪"
			result.Source, result.MACVerified = "mac", true
			result.CheckedAt = time.Now().UTC()
			return result
		}
		if ctx.Err() != nil {
			result.State, result.Reason = "unknown", "检测超时，请稍后重试"
			break
		}
		result.State, result.Reason = "unresponsive", "未收到探测响应：可能关机、休眠，或被防火墙拦截"
	}
	result.CheckedAt = time.Now().UTC()
	return result
}

// TCP success/refusal and UDP replies/port-unreachable errors are actual
// responses. A successful write, cached ARP entry, or timeout is not evidence.
// No raw socket, shell, privileged helper, or per-device agent is required.
func probeStatus(ctx context.Context, network Network, ip string) (string, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	answers := make(chan string, 5)
	var wg sync.WaitGroup
	for _, port := range []int{22, 443, 445, 3389} {
		wg.Add(1)
		go func(port int) {
			defer wg.Done()
			if tcpStatusProbe(ctx, network.IP, ip, port) {
				answers <- "TCP"
			}
		}(port)
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		dialer := net.Dialer{LocalAddr: &net.UDPAddr{IP: net.ParseIP(network.IP)}}
		conn, err := dialer.DialContext(ctx, "udp4", net.JoinHostPort(ip, "9"))
		if err != nil {
			return
		}
		defer conn.Close()
		stop := context.AfterFunc(ctx, func() { conn.Close() })
		defer stop()
		if deadline, ok := ctx.Deadline(); ok {
			conn.SetDeadline(deadline)
		}
		if _, err := conn.Write([]byte{0}); err != nil {
			return
		}
		buffer := make([]byte, 64)
		if _, err := conn.Read(buffer); err == nil || errors.Is(err, syscall.ECONNREFUSED) {
			answers <- "UDP"
		}
	}()
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case method := <-answers:
		cancel()
		<-done
		return method, nil
	case <-done:
		select {
		case method := <-answers:
			return method, nil
		default:
			return "", errors.New("no response")
		}
	case <-ctx.Done():
		cancel()
		<-done
		return "", ctx.Err()
	}
}

func tcpStatusProbe(ctx context.Context, local, remote string, port int) bool {
	dialer := net.Dialer{LocalAddr: &net.TCPAddr{IP: net.ParseIP(local)}}
	conn, err := dialer.DialContext(ctx, "tcp4", net.JoinHostPort(remote, strconv.Itoa(port)))
	if conn != nil {
		conn.Close()
	}
	return err == nil || errors.Is(err, syscall.ECONNREFUSED)
}
