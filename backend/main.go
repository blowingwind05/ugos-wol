package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

//go:embed web/*
var assets embed.FS

const api = "/api/wol/v1/"
const version = "0.2.4"

type User struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Role string `json:"role"`
}
type userContextKey struct{}

var lastIdentityDiagnostic atomic.Int64

// Diagnostic metadata only: never log token, cookie, or arbitrary header values.
func logIdentityFailure(r *http.Request) {
	now := time.Now().Unix()
	last := lastIdentityDiagnostic.Load()
	if now-last < 15 || !lastIdentityDiagnostic.CompareAndSwap(last, now) {
		return
	}
	names := make([]string, 0, len(r.Header))
	for name := range r.Header {
		names = append(names, name)
	}
	sort.Strings(names)
	log.Printf("UGOS identity missing: id_count=%d name_count=%d role_count=%d token_present=%t header_names=%q", len(r.Header.Values("Ugreen-User-ID")), len(r.Header.Values("Ugreen-User-Name")), len(r.Header.Values("Ugreen-User-Type")), r.Header.Get("Ugreen-Ttk") != "", names)
}

type Device struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	MAC     string `json:"mac"`
	Network string `json:"network"`
}
type Network struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	IP        string `json:"ip"`
	CIDR      string `json:"cidr"`
	Broadcast string `json:"broadcast"`
}
type Event struct {
	Time    time.Time `json:"time"`
	Name    string    `json:"name"`
	MAC     string    `json:"mac"`
	Sent    int       `json:"sent"`
	Targets []string  `json:"targets"`
	Error   string    `json:"error,omitempty"`
	Actor   *User     `json:"actor,omitempty"`
}
type State struct {
	Devices []Device `json:"devices"`
	History []Event  `json:"history"`
}
type App struct {
	mu        sync.Mutex
	state     State
	file      string
	lastWake  time.Time
	groupFile string
	networks  func() ([]Network, error)
	send      func([]byte, Network, int) (int, error)
}

func usableLANInterface(nic net.Interface) bool {
	if nic.Flags&net.FlagUp == 0 || nic.Flags&net.FlagBroadcast == 0 || nic.Flags&net.FlagLoopback != 0 {
		return false
	}
	for _, prefix := range []string{"docker", "veth", "virbr"} {
		if strings.HasPrefix(nic.Name, prefix) {
			return false
		}
	}
	// Docker uses br- followed by a 12-digit hexadecimal network ID.
	// Keep named LAN bridges (for example br-lan) available to administrators.
	if suffix := strings.TrimPrefix(nic.Name, "br-"); suffix != nic.Name && len(suffix) == 12 {
		if _, err := hex.DecodeString(suffix); err == nil {
			return false
		}
	}
	return true
}

func localNetworks() ([]Network, error) {
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	result := []Network{}
	for _, nic := range interfaces {
		if !usableLANInterface(nic) {
			continue
		}
		addrs, err := nic.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			ipnet, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}
			ip := ipnet.IP.To4()
			if ip == nil || !ip.IsGlobalUnicast() || ip.IsLinkLocalUnicast() {
				continue
			}
			ones, bits := ipnet.Mask.Size()
			if bits != 32 || ones > 30 {
				continue
			}
			network := ip.Mask(ipnet.Mask)
			broadcast := make(net.IP, 4)
			for i := range broadcast {
				broadcast[i] = network[i] | ^ipnet.Mask[i]
			}
			cidr := fmt.Sprintf("%s/%d", network, ones)
			result = append(result, Network{nic.Name + "/" + cidr, nic.Name, ip.String(), cidr, broadcast.String()})
		}
	}
	return result, nil
}

func magicPacket(mac string) ([]byte, string, error) {
	addr, err := net.ParseMAC(strings.TrimSpace(mac))
	if err != nil || len(addr) != 6 {
		return nil, "", errors.New("请输入 6 字节的 MAC 地址，例如 AA:BB:CC:DD:EE:FF")
	}
	if addr[0]&1 != 0 || strings.ReplaceAll(addr.String(), ":", "") == "000000000000" {
		return nil, "", errors.New("MAC 地址必须是有效的单播网卡地址")
	}
	packet := make([]byte, 102)
	for i := 0; i < 6; i++ {
		packet[i] = 0xff
	}
	for i := 0; i < 16; i++ {
		copy(packet[6+i*6:], addr)
	}
	return packet, strings.ToUpper(addr.String()), nil
}

func sendPacket(packet []byte, network Network, port int) (int, error) {
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP(network.IP)})
	if err != nil {
		return 0, err
	}
	defer conn.Close()
	raw, err := conn.SyscallConn()
	if err != nil {
		return 0, err
	}
	var sockErr error
	err = raw.Control(func(fd uintptr) {
		sockErr = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_BROADCAST, 1)
	})
	if err != nil {
		return 0, err
	}
	if sockErr != nil {
		return 0, sockErr
	}
	if err := conn.SetWriteDeadline(time.Now().Add(2 * time.Second)); err != nil {
		return 0, err
	}
	target := &net.UDPAddr{IP: net.ParseIP(network.Broadcast), Port: port}
	sent := 0
	for i := 0; i < 3; i++ {
		n, err := conn.WriteToUDP(packet, target)
		if err != nil {
			return sent, err
		}
		if n != len(packet) {
			return sent, io.ErrShortWrite
		}
		sent++
		if i < 2 {
			time.Sleep(120 * time.Millisecond)
		}
	}
	return sent, nil
}

func newApp(dir string) (*App, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	a := &App{file: filepath.Join(dir, "state.json"), groupFile: "/etc/group", networks: localNetworks, send: sendPacket,
		state: State{Devices: []Device{}, History: []Event{}}}
	data, err := os.ReadFile(a.file)
	if err == nil {
		if err = json.Unmarshal(data, &a.state); err != nil {
			return nil, fmt.Errorf("配置文件无法读取，请备份后修复（不会覆盖原文件）: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if a.state.Devices == nil {
		a.state.Devices = []Device{}
	}
	if a.state.History == nil {
		a.state.History = []Event{}
	}
	return a, nil
}

// save atomically replaces the state only after the complete new file is synced.
func (a *App) save(next State) error {
	data, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(a.file), ".state-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = os.Rename(f.Name(), a.file); err != nil {
		return err
	}
	a.state = next
	return nil
}

func respond(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func problem(w http.ResponseWriter, status int, message string) {
	respond(w, status, map[string]string{"error": message})
}
func decode(w http.ResponseWriter, r *http.Request, out any) bool {
	if strings.Split(r.Header.Get("Content-Type"), ";")[0] != "application/json" {
		problem(w, 415, "请使用 JSON 请求")
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		problem(w, 400, "请求内容无效或过长")
		return false
	}
	if err := d.Decode(new(any)); err != io.EOF {
		problem(w, 400, "请求只能包含一个 JSON 对象")
		return false
	}
	return true
}

// UGOS verifies Ugreen-Ttk at the gateway and replaces these identity headers.
// Trust that local gateway only: never expose this listener to the LAN or accept
// forwarded IP headers as evidence of a local peer. Local OS processes remain
// inside the NAS trust boundary, as with the documented native-app integration.
func (a *App) gatewayUser(w http.ResponseWriter, r *http.Request) (User, bool) {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	peer := net.ParseIP(host)
	if err != nil || peer == nil || !peer.IsLoopback() {
		problem(w, 403, "请通过 UGOS 应用网关访问")
		return User{}, false
	}
	for _, key := range []string{"Ugreen-User-ID", "Ugreen-User-Name"} {
		if len(r.Header.Values(key)) != 1 || strings.TrimSpace(r.Header.Get(key)) == "" {
			logIdentityFailure(r)
			problem(w, 401, "无法确认 NAS 登录身份，请从 UGOS 桌面重新打开应用")
			return User{}, false
		}
	}
	user := User{ID: r.Header.Get("Ugreen-User-ID"), Name: r.Header.Get("Ugreen-User-Name"), Role: r.Header.Get("Ugreen-User-Type")}
	if len(user.ID) > 128 || strings.ContainsAny(user.ID, ", \t\r\n") || len(user.Name) > 256 {
		problem(w, 401, "NAS 登录身份无效，请重新登录")
		return User{}, false
	}
	roles := r.Header.Values("Ugreen-User-Type")
	if len(roles) > 1 || (len(roles) == 1 && strings.TrimSpace(user.Role) == "") {
		problem(w, 401, "NAS 用户角色信息无效，请重新登录")
		return User{}, false
	}
	if len(roles) == 0 {
		// Some UGOS releases provide verified ID/name but omit the role.
		// Require a separate server-side administrator membership check.
		admin, err := a.localAdministrator(user.Name)
		if err != nil {
			problem(w, 503, "该固件未提供用户角色，暂时无法读取 NAS 管理员组，请检查应用兼容配置")
			return User{}, false
		}
		if admin {
			user.Role = "admin"
		}
	}
	if user.Role != "admin" {
		problem(w, 403, "当前 NAS 用户没有权限，仅管理员可使用远程唤醒")
		return User{}, false
	}
	return user, true
}

func (a *App) localAdministrator(name string) (bool, error) {
	f, err := os.Open(a.groupFile)
	if err != nil {
		return false, err
	}
	defer f.Close()
	scanner := bufio.NewScanner(io.LimitReader(f, 1<<20))
	found, member := false, false
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "#") || strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.Split(line, ":")
		if fields[0] != "admin" {
			continue
		}
		if found || len(fields) != 4 {
			return false, errors.New("invalid admin group record")
		}
		found = true
		for _, entry := range strings.Split(fields[3], ",") {
			if entry == name {
				member = true
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return false, err
	}
	if !found {
		return false, errors.New("administrator group unavailable")
	}
	return member, nil
}

func (a *App) handler() http.Handler {
	web, _ := fs.Sub(assets, "web")
	files := http.FileServer(http.FS(web))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; base-uri 'self'; form-action 'self'; object-src 'none'")
		if !strings.HasPrefix(r.URL.Path, api) {
			if r.Method != "GET" && r.Method != "HEAD" {
				problem(w, 405, "请求方法不支持")
				return
			}
			files.ServeHTTP(w, r)
			return
		}
		user, ok := a.gatewayUser(w, r)
		if !ok {
			return
		}
		a.route(w, r.WithContext(context.WithValue(r.Context(), userContextKey{}, user)))
	})
}

func (a *App) route(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, api)
	switch {
	case path == "info" && r.Method == "GET":
		networks, err := a.networks()
		if err != nil {
			problem(w, 500, "无法读取 NAS 网络接口")
			return
		}
		a.mu.Lock()
		defer a.mu.Unlock()
		respond(w, 200, map[string]any{"version": version, "user": r.Context().Value(userContextKey{}), "devices": a.state.Devices, "history": a.state.History, "networks": networks})
	case path == "devices" && r.Method == "POST":
		a.writeDevice(w, r, "")
	case strings.HasPrefix(path, "devices/") && !strings.Contains(strings.TrimPrefix(path, "devices/"), "/") && r.Method == "PUT":
		a.writeDevice(w, r, strings.TrimPrefix(path, "devices/"))
	case strings.HasPrefix(path, "devices/") && r.Method == "DELETE":
		id := strings.TrimPrefix(path, "devices/")
		a.mu.Lock()
		defer a.mu.Unlock()
		next := State{Devices: []Device{}, History: a.state.History}
		found := false
		for _, d := range a.state.Devices {
			if d.ID == id {
				found = true
			} else {
				next.Devices = append(next.Devices, d)
			}
		}
		if !found {
			problem(w, 404, "设备不存在")
			return
		}
		if err := a.save(next); err != nil {
			problem(w, 500, "保存失败，请检查数据目录权限和剩余空间")
			return
		}
		respond(w, 200, map[string]bool{"ok": true})
	case strings.HasPrefix(path, "wake/") && r.Method == "POST":
		var body struct{}
		if !decode(w, r, &body) {
			return
		}
		a.wake(w, r, strings.TrimPrefix(path, "wake/"))
	default:
		problem(w, 404, "接口不存在或请求方法不支持")
	}
}

func (a *App) writeDevice(w http.ResponseWriter, r *http.Request, id string) {
	var input struct {
		Name    string `json:"name"`
		MAC     string `json:"mac"`
		Network string `json:"network"`
	}
	if !decode(w, r, &input) {
		return
	}
	input.Name = strings.TrimSpace(input.Name)
	if len([]rune(input.Name)) < 1 || len([]rune(input.Name)) > 40 {
		problem(w, 400, "设备名称需为 1–40 个字符")
		return
	}
	_, mac, err := magicPacket(input.MAC)
	if err != nil {
		problem(w, 400, err.Error())
		return
	}
	networks, err := a.networks()
	if err != nil {
		problem(w, 500, "无法读取 NAS 网络接口")
		return
	}
	found := input.Network == "auto"
	for _, n := range networks {
		if n.ID == input.Network {
			found = true
		}
	}
	if !found {
		problem(w, 400, "所选网络接口已不可用，请重新选择")
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	next := State{Devices: append([]Device{}, a.state.Devices...), History: a.state.History}
	index := -1
	for i, d := range next.Devices {
		if d.ID == id {
			index = i
		}
		if d.MAC == mac && d.ID != id {
			problem(w, 409, "该 MAC 地址已经添加")
			return
		}
	}
	if id != "" && index < 0 {
		problem(w, 404, "设备不存在")
		return
	}
	if id == "" {
		if len(next.Devices) >= 32 {
			problem(w, 400, "最多可保存 32 台设备")
			return
		}
		var bytes [16]byte
		if _, err := rand.Read(bytes[:]); err != nil {
			problem(w, 500, "无法生成设备标识")
			return
		}
		id = hex.EncodeToString(bytes[:])
	}
	device := Device{ID: id, Name: input.Name, MAC: mac, Network: input.Network}
	if index >= 0 {
		next.Devices[index] = device
	} else {
		next.Devices = append(next.Devices, device)
	}
	if err := a.save(next); err != nil {
		problem(w, 500, "保存失败，请检查数据目录权限和剩余空间")
		return
	}
	respond(w, 200, device)
}

func (a *App) wake(w http.ResponseWriter, r *http.Request, id string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	var device Device
	for _, d := range a.state.Devices {
		if d.ID == id {
			device = d
			break
		}
	}
	if device.ID == "" {
		problem(w, 404, "设备不存在")
		return
	}
	if time.Since(a.lastWake) < 3*time.Second {
		w.Header().Set("Retry-After", "3")
		problem(w, 429, "发送过于频繁，请稍等 3 秒再试")
		return
	}
	networks, err := a.networks()
	if err != nil {
		problem(w, 500, "无法读取 NAS 网络接口")
		return
	}
	selected := []Network{}
	for _, n := range networks {
		if device.Network == "auto" || device.Network == n.ID {
			selected = append(selected, n)
		}
	}
	if len(selected) == 0 {
		problem(w, 409, "没有可用的局域网接口，请检查 NAS 网络或编辑设备")
		return
	}
	packet, _, err := magicPacket(device.MAC)
	if err != nil {
		problem(w, 400, err.Error())
		return
	}
	a.lastWake = time.Now()
	user := r.Context().Value(userContextKey{}).(User)
	event := Event{Time: time.Now().UTC(), Name: device.Name, MAC: device.MAC, Targets: []string{}, Actor: &user}
	errorsList := []string{}
	for _, n := range selected {
		count, err := a.send(packet, n, 9)
		event.Sent += count
		if count > 0 {
			event.Targets = append(event.Targets, n.Name+" → "+n.Broadcast+":9")
		}
		if err != nil {
			errorsList = append(errorsList, n.Name+": "+err.Error())
		}
	}
	event.Error = strings.Join(errorsList, "; ")
	next := State{Devices: a.state.Devices, History: append([]Event{event}, a.state.History...)}
	if len(next.History) > 100 {
		next.History = next.History[:100]
	}
	logSaved := true
	if err := a.save(next); err != nil {
		logSaved = false
		a.state = next
		log.Printf("history persistence failed: %v", err)
	}
	status := 200
	message := "唤醒包已发送，请通过目标设备或对应管理工具确认唤醒结果"
	if event.Sent == 0 {
		status = 502
		message = "唤醒包发送失败，请查看发送记录"
	} else if event.Error != "" {
		message = "部分接口发送成功，另有接口失败，请查看发送记录"
	}
	if !logSaved {
		message += "；记录未能写入磁盘，请检查 NAS 数据目录"
	}
	respond(w, status, map[string]any{"message": message, "event": event, "log_saved": logSaved})
}

func main() {
	port := flag.Int("port", 28089, "HTTP port")
	bind := flag.String("bind", "127.0.0.1", "HTTP bind address")
	dir := flag.String("data", "", "data directory (defaults to UGAPP_DATA_DIR or ./data)")
	flag.Parse()
	if ip := net.ParseIP(*bind); ip == nil || !ip.IsLoopback() {
		log.Fatal("身份认证模式仅允许监听回环地址，由 UGOS 网关代理")
	}
	if *dir == "" {
		*dir = os.Getenv("UGAPP_DATA_DIR")
	}
	if *dir == "" {
		*dir = "data"
	}
	a, err := newApp(*dir)
	if err != nil {
		log.Fatal(err)
	}
	server := &http.Server{Addr: net.JoinHostPort(*bind, fmt.Sprint(*port)), Handler: a.handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 20 * time.Second, IdleTimeout: 45 * time.Second, MaxHeaderBytes: 16 * 1024}
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-stop
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
	}()
	log.Printf("UGOS WOL %s listening on %s", version, server.Addr)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}
