package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const testIdentity = "admin"

var testNetwork = Network{"eth0/192.168.12.0/23", "eth0", "192.168.12.10", "192.168.12.0/23", "192.168.13.255"}

func fixture(t *testing.T) *App {
	t.Helper()
	a, err := newApp(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a.groupFile = filepath.Join(t.TempDir(), "group")
	if err := os.WriteFile(a.groupFile, []byte("admin:x:10:测试管理员,管理员\nusers:x:100:普通用户\n"), 0600); err != nil {
		t.Fatal(err)
	}
	a.networks = func() ([]Network, error) { return []Network{testNetwork}, nil }
	a.send = func(packet []byte, n Network, port int) (int, error) {
		if n.Broadcast != "192.168.13.255" || port != 9 || len(packet) != 102 {
			t.Error("wrong packet or destination")
		}
		return 3, nil
	}
	return a
}
func call(t *testing.T, a *App, method, path, body, identity string) (int, []byte) {
	t.Helper()
	r := httptest.NewRequest(method, api+path, strings.NewReader(body))
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	r.RemoteAddr = "127.0.0.1:12345"
	if identity != "" {
		r.Header.Set("Ugreen-User-ID", "100")
		r.Header.Set("Ugreen-User-Name", "测试管理员")
		r.Header.Set("Ugreen-User-Type", identity)
	}
	w := httptest.NewRecorder()
	a.handler().ServeHTTP(w, r)
	return w.Code, w.Body.Bytes()
}
func add(t *testing.T, a *App) Device {
	t.Helper()
	status, body := call(t, a, "POST", "devices", `{"name":"笔记本","mac":"02-AB-CD-EF-12-34","network":"auto"}`, testIdentity)
	if status != 200 {
		t.Fatalf("add: %d %s", status, body)
	}
	var d Device
	if err := json.Unmarshal(body, &d); err != nil {
		t.Fatal(err)
	}
	return d
}

func TestMagicPacketOnWire(t *testing.T) {
	packet, normalized, err := magicPacket("02-ab-cd-ef-12-34")
	if err != nil || normalized != "02:AB:CD:EF:12:34" {
		t.Fatal(normalized, err)
	}
	expected := append(bytes.Repeat([]byte{255}, 6), bytes.Repeat([]byte{2, 171, 205, 239, 18, 52}, 16)...)
	if !bytes.Equal(packet, expected) {
		t.Fatalf("invalid magic packet: %x", packet)
	}
	listener, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	done := make(chan error, 1)
	go func() {
		_ = listener.SetReadDeadline(time.Now().Add(3 * time.Second))
		for i := 0; i < 3; i++ {
			buf := make([]byte, 512)
			n, _, err := listener.ReadFromUDP(buf)
			if err != nil {
				done <- err
				return
			}
			if !bytes.Equal(buf[:n], expected) {
				done <- errors.New("UDP payload mismatch")
				return
			}
		}
		done <- nil
	}()
	sent, err := sendPacket(packet, Network{IP: "127.0.0.1", Broadcast: "127.0.0.1"}, listener.LocalAddr().(*net.UDPAddr).Port)
	if err != nil || sent != 3 {
		t.Fatal(sent, err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
func TestRejectInvalidMACs(t *testing.T) {
	for _, s := range []string{"bad", "FF:FF:FF:FF:FF:FF", "00:00:00:00:00:00", "01:00:5E:00:00:01", "00:11:22:33:44:55:66:77"} {
		if _, _, err := magicPacket(s); err == nil {
			t.Errorf("accepted %q", s)
		}
	}
}
func TestGatewayIdentityAndPermissions(t *testing.T) {
	a := fixture(t)
	cases := []struct {
		name, peer, id, user, role string
		duplicate                  bool
		want                       int
	}{
		{"no identity", "127.0.0.1:1234", "", "", "", false, 401},
		{"ordinary user", "127.0.0.1:1234", "101", "普通用户", "users", false, 403},
		{"unknown role", "127.0.0.1:1234", "101", "普通用户", "superadmin", false, 403},
		{"missing ID", "127.0.0.1:1234", "", "管理员", "admin", false, 401},
		{"empty name", "127.0.0.1:1234", "100", "", "admin", false, 401},
		{"duplicate ID", "127.0.0.1:1234", "100", "管理员", "admin", true, 401},
		{"combined IDs", "127.0.0.1:1234", "100,101", "管理员", "admin", false, 401},
		{"forged LAN headers", "192.168.1.9:1234", "100", "管理员", "admin", false, 403},
		{"invalid peer", "broken", "100", "管理员", "admin", false, 403},
		{"gateway admin", "127.0.0.1:1234", "100", "管理员", "admin", false, 200},
		{"IPv6 loopback gateway", "[::1]:1234", "100", "管理员", "admin", false, 200},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", api+"info", nil)
			r.RemoteAddr = tc.peer
			if tc.id != "" {
				r.Header.Set("Ugreen-User-ID", tc.id)
			}
			if tc.user != "" {
				r.Header.Set("Ugreen-User-Name", tc.user)
			}
			if tc.role != "" {
				r.Header.Set("Ugreen-User-Type", tc.role)
			}
			if tc.duplicate {
				r.Header.Add("Ugreen-User-ID", "101")
			}
			r.Header.Set("X-Forwarded-For", "127.0.0.1")
			r.Header.Set("Ugreen-Ttk", "client-token-is-not-server-identity")
			r.Header.Set("Authorization", "Bearer old-password-must-not-authenticate")
			w := httptest.NewRecorder()
			a.handler().ServeHTTP(w, r)
			if w.Code != tc.want {
				t.Fatalf("want %d, got %d: %s", tc.want, w.Code, w.Body.String())
			}
			if w.Code == 200 && !strings.Contains(w.Body.String(), `"user":{"id":"100","name":"管理员","role":"admin"}`) {
				t.Fatal(w.Body.String())
			}
		})
	}
}
func TestDeviceLifecyclePersistsAndWakeCooldown(t *testing.T) {
	a := fixture(t)
	d := add(t, a)
	if d.MAC != "02:AB:CD:EF:12:34" || len(d.ID) != 32 {
		t.Fatal(d)
	}
	if status, _ := call(t, a, "POST", "devices", `{"name":"duplicate","mac":"02:AB:CD:EF:12:34","network":"auto"}`, testIdentity); status != 409 {
		t.Fatal(status)
	}
	status, body := call(t, a, "POST", "wake/"+d.ID, `{}`, testIdentity)
	if status != 200 || !bytes.Contains(body, []byte(`"sent":3`)) {
		t.Fatalf("wake: %d %s", status, body)
	}
	if status, _ := call(t, a, "POST", "wake/"+d.ID, `{}`, testIdentity); status != 429 {
		t.Fatal(status)
	}
	b, err := newApp(filepath.Dir(a.file))
	if err != nil {
		t.Fatal(err)
	}
	if len(b.state.Devices) != 1 || len(b.state.History) != 1 || b.state.History[0].Sent != 3 {
		t.Fatal(b.state)
	}
	if status, _ := call(t, a, "PUT", "devices/"+d.ID, `{"name":"修改名称","mac":"02:AB:CD:EF:12:34","network":"auto"}`, testIdentity); status != 200 {
		t.Fatal(status)
	}
	if status, _ := call(t, a, "DELETE", "devices/"+d.ID, "", testIdentity); status != 200 {
		t.Fatal(status)
	}
	if len(a.state.Devices) != 0 || len(a.state.History) != 1 {
		t.Fatal(a.state)
	}
}
func TestDestinationAndInputValidation(t *testing.T) {
	a := fixture(t)
	for _, body := range []string{
		`{"name":"laptop","mac":"02:AB:CD:EF:12:34","network":"8.8.8.8"}`,
		`{"name":"","mac":"02:AB:CD:EF:12:34","network":"auto"}`,
		`{"name":"laptop","mac":"02:AB:CD:EF:12:34","network":"auto","command":"whoami"}`,
		`{"name":"laptop","mac":"02:AB:CD:EF:12:34","network":"auto"} {}`,
	} {
		if status, _ := call(t, a, "POST", "devices", body, testIdentity); status != 400 {
			t.Errorf("%d %s", status, body)
		}
	}
	d := add(t, a)
	a.networks = func() ([]Network, error) { return nil, nil }
	if status, _ := call(t, a, "POST", "wake/"+d.ID, `{}`, testIdentity); status != 409 {
		t.Fatal(status)
	}
	if len(a.state.History) != 0 {
		t.Fatal("no transmission should not create a sent entry")
	}
}
func TestSendFailureAndHistoryWriteFailureAreTruthful(t *testing.T) {
	a := fixture(t)
	d := add(t, a)
	a.send = func([]byte, Network, int) (int, error) { return 0, errors.New("permission denied") }
	status, body := call(t, a, "POST", "wake/"+d.ID, `{}`, testIdentity)
	if status != 502 || !bytes.Contains(body, []byte(`"sent":0`)) || len(a.state.History) != 1 {
		t.Fatalf("%d %s", status, body)
	}
	a.lastWake = time.Time{}
	a.send = func([]byte, Network, int) (int, error) { return 3, nil }
	a.file = filepath.Join(a.file, "missing", "state.json")
	status, body = call(t, a, "POST", "wake/"+d.ID, `{}`, testIdentity)
	if status != 200 || !bytes.Contains(body, []byte(`"log_saved":false`)) || !bytes.Contains(body, []byte(`"sent":3`)) {
		t.Fatalf("%d %s", status, body)
	}
}
func TestCorruptStateIsNeverOverwritten(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "state.json")
	if err := os.WriteFile(file, []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := newApp(dir); err == nil {
		t.Fatal("corrupt state accepted")
	}
	content, err := os.ReadFile(file)
	if err != nil || string(content) != "broken" {
		t.Fatal("corrupt state overwritten")
	}
}
func TestWakeRecordsCurrentIdentityAndLoadsLegacyData(t *testing.T) {
	a := fixture(t)
	d := add(t, a)
	status, body := call(t, a, "POST", "wake/"+d.ID, `{}`, testIdentity)
	if status != 200 {
		t.Fatalf("%d %s", status, body)
	}
	actor := a.state.History[0].Actor
	if actor == nil || actor.ID != "100" || actor.Name != "测试管理员" {
		t.Fatal(actor)
	}
	a.lastWake = time.Time{}
	r := httptest.NewRequest("POST", api+"wake/"+d.ID, strings.NewReader(`{}`))
	r.RemoteAddr = "127.0.0.1:1234"
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Ugreen-User-ID", "200")
	r.Header.Set("Ugreen-User-Name", "另一管理员")
	r.Header.Set("Ugreen-User-Type", "admin")
	w := httptest.NewRecorder()
	a.handler().ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if a.state.History[0].Actor.ID != "200" || a.state.History[1].Actor.ID != "100" {
		t.Fatal("identity leaked between requests")
	}
	legacyDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(legacyDir, "state.json"), []byte(`{"devices":[],"history":[{"name":"旧设备","mac":"02:11:22:33:44:55","sent":3,"targets":[]}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	legacy, err := newApp(legacyDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(legacy.state.History) != 1 || legacy.state.History[0].Actor != nil {
		t.Fatal("legacy records incompatible")
	}
}
func TestStaticUIAndJSONBodyLimit(t *testing.T) {
	a := fixture(t)
	server := httptest.NewServer(a.handler())
	defer server.Close()
	r, err := http.Get(server.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	body, _ := io.ReadAll(r.Body)
	if r.StatusCode != 200 || !bytes.Contains(body, []byte("远程唤醒")) {
		t.Fatal(r.StatusCode)
	}
	if status, _ := call(t, a, "POST", "devices", `{"name":"`+strings.Repeat("x", 9000)+`"}`, testIdentity); status != 400 {
		t.Fatal(status)
	}
}

func TestLegacyGatewayRoleUsesAdministratorGroup(t *testing.T) {
	a := fixture(t)
	check := func(name string, id string, role *string) int {
		r := httptest.NewRequest("GET", api+"info", nil)
		r.RemoteAddr = "127.0.0.1:1234"
		if id != "" {
			r.Header.Set("Ugreen-User-ID", id)
		}
		r.Header.Set("Ugreen-User-Name", name)
		if role != nil {
			r.Header.Set("Ugreen-User-Type", *role)
		}
		w := httptest.NewRecorder()
		a.handler().ServeHTTP(w, r)
		return w.Code
	}
	if code := check("测试管理员", "100", nil); code != 200 {
		t.Fatalf("legacy admin: %d", code)
	}
	if code := check("普通用户", "101", nil); code != 403 {
		t.Fatalf("legacy ordinary user: %d", code)
	}
	if code := check("测试管理", "102", nil); code != 403 {
		t.Fatalf("partial name matched: %d", code)
	}
	if code := check("测试管理员", "", nil); code != 401 {
		t.Fatalf("name only authorized: %d", code)
	}
	role := "users"
	if code := check("测试管理员", "100", &role); code != 403 {
		t.Fatalf("explicit ordinary role overridden: %d", code)
	}
	role = ""
	if code := check("测试管理员", "100", &role); code != 401 {
		t.Fatalf("empty role accepted: %d", code)
	}
	if err := os.WriteFile(a.groupFile, []byte("admin:x:10:\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if code := check("测试管理员", "100", nil); code != 403 {
		t.Fatalf("removed group member accepted: %d", code)
	}
	if err := os.WriteFile(a.groupFile, []byte("admin:x:10:测试管理员\nadmin:x:10:普通用户\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if code := check("测试管理员", "100", nil); code != 503 {
		t.Fatalf("ambiguous group accepted: %d", code)
	}
	a.groupFile = filepath.Join(t.TempDir(), "absent")
	if code := check("测试管理员", "100", nil); code != 503 {
		t.Fatalf("missing group file accepted: %d", code)
	}
	role = "admin"
	if code := check("测试管理员", "100", &role); code != 200 {
		t.Fatalf("modern role incorrectly requires group file: %d", code)
	}
}
