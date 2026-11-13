package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func parsedTime(t *testing.T, value string) time.Time {
	t.Helper()
	v, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t.Fatal(err)
	}
	return v
}
func taskFixture(d Device, due time.Time, repeat string) Task {
	return Task{ID: "task-" + d.ID, TaskInput: TaskInput{Name: "定时唤醒", DeviceID: d.ID, Repeat: repeat, Date: due.Format("2006-01-02"), Time: due.Format("15:04"), Timezone: "UTC", Enabled: true}, Author: User{ID: "100", Name: "测试管理员", Role: "admin"}, NextRun: &due}
}

func TestTaskCalendarRulesAndTimezone(t *testing.T) {
	for _, tc := range []struct {
		repeat, clock, zone, after, want string
		days                             []int
	}{
		{"weekdays", "08:30", "Asia/Shanghai", "2026-11-13T01:00:00Z", "2026-11-16T00:30:00Z", nil},
		{"weekends", "09:00", "Asia/Shanghai", "2026-11-13T20:00:00Z", "2026-11-14T01:00:00Z", nil},
		{"weekends", "09:00", "Asia/Shanghai", "2026-11-15T02:00:00Z", "2026-11-21T01:00:00Z", nil},
		{"custom", "10:15", "UTC", "2026-11-12T10:15:00Z", "2026-11-17T10:15:00Z", []int{2, 4}},
		{"daily", "00:01", "Asia/Tokyo", "2026-11-12T15:00:00Z", "2026-11-12T15:01:00Z", nil},
		{"custom", "02:30", "America/New_York", "2026-03-08T05:00:00Z", "2026-03-15T06:30:00Z", []int{7}},
	} {
		input := TaskInput{Repeat: tc.repeat, Time: tc.clock, Timezone: tc.zone, Weekdays: tc.days}
		next, err := taskOccurrence(input, parsedTime(t, tc.after))
		if err != nil || next == nil || !next.Equal(parsedTime(t, tc.want)) {
			t.Fatal(tc, next, err)
		}
	}
	input := TaskInput{Repeat: "once", Date: "2026-11-13", Time: "08:30", Timezone: "Asia/Shanghai"}
	next, err := taskOccurrence(input, parsedTime(t, "2026-11-12T00:00:00Z"))
	if err != nil || !next.Equal(parsedTime(t, "2026-11-13T00:30:00Z")) {
		t.Fatal(next, err)
	}
	if next, err = taskOccurrence(input, *next); err != nil || next != nil {
		t.Fatal("one-shot already elapsed", next, err)
	}
	input = TaskInput{Repeat: "custom", Weekdays: []int{7}, Time: "01:30", Timezone: "America/New_York"}
	first, err := taskOccurrence(input, parsedTime(t, "2026-11-01T04:00:00Z"))
	if err != nil {
		t.Fatal(err)
	}
	following, err := taskOccurrence(input, *first)
	if err != nil || following.Sub(*first) < 6*24*time.Hour {
		t.Fatal("fall-back hour repeated", first, following, err)
	}
	input = TaskInput{Repeat: "once", Date: "2026-03-08", Time: "02:30", Timezone: "America/New_York"}
	if _, err = taskOccurrence(input, parsedTime(t, "2026-03-01T00:00:00Z")); err == nil {
		t.Fatal("nonexistent DST time accepted")
	}
}

func TestTaskValidationAndAuthorization(t *testing.T) {
	a := fixture(t)
	d := add(t, a)
	valid := TaskInput{Name: "工作日唤醒", DeviceID: d.ID, Repeat: "weekdays", Time: "08:30", Timezone: "Asia/Shanghai", Enabled: true}
	for _, who := range []string{"", "users"} {
		body, _ := json.Marshal(valid)
		code, _ := call(t, a, "POST", "tasks", string(body), who)
		if (who == "" && code != 401) || (who == "users" && code != 403) {
			t.Fatal(code)
		}
	}
	for _, change := range []func(*TaskInput){
		func(i *TaskInput) { i.Repeat = "unknown" }, func(i *TaskInput) { i.Time = "24:00" }, func(i *TaskInput) { i.Time = "9:00" },
		func(i *TaskInput) { i.Timezone = "Local" }, func(i *TaskInput) { i.Timezone = "/etc/passwd" }, func(i *TaskInput) { i.DeviceID = "missing" },
		func(i *TaskInput) { i.Repeat = "custom"; i.Weekdays = nil }, func(i *TaskInput) { i.Repeat = "custom"; i.Weekdays = []int{2, 2} }, func(i *TaskInput) { i.Repeat = "custom"; i.Weekdays = []int{8} },
		func(i *TaskInput) { i.Repeat = "once"; i.Date = "2026-02-30" }, func(i *TaskInput) { i.Repeat = "once"; i.Date = "2000-01-01" }, func(i *TaskInput) { i.Name = strings.Repeat("字", 41) },
	} {
		input := valid
		change(&input)
		body, _ := json.Marshal(input)
		if code, data := call(t, a, "POST", "tasks", string(body), "admin"); code != 400 {
			t.Fatal(input, code, string(data))
		}
	}
	if code, _ := call(t, a, "POST", "tasks", `{"device_id":"x","command":"poweroff"}`, "admin"); code != 400 {
		t.Fatal(code)
	}
	body, _ := json.Marshal(valid)
	code, data := call(t, a, "POST", "tasks", string(body), "admin")
	if code != 200 {
		t.Fatal(code, string(data))
	}
	var task Task
	if err := json.Unmarshal(data, &task); err != nil || task.Author.ID != "100" || task.NextRun == nil || len(task.ID) != 32 {
		t.Fatal(task, err)
	}
	if code, _ := call(t, a, "DELETE", "devices/"+d.ID, "", "admin"); code != 409 {
		t.Fatal("device with tasks deleted", code)
	}
	valid.Enabled = false
	body, _ = json.Marshal(valid)
	if code, data = call(t, a, "PUT", "tasks/"+task.ID, string(body), "admin"); code != 200 {
		t.Fatal(code, string(data))
	}
	task = Task{}
	if err := json.Unmarshal(data, &task); err != nil {
		t.Fatal(err)
	}
	if task.NextRun != nil || task.Enabled {
		t.Fatal(task)
	}
	if code, _ := call(t, a, "PUT", "devices/"+d.ID, `{"name":"改名","mac":"`+d.MAC+`","network":"auto"}`, "admin"); code != 200 {
		t.Fatal(code)
	}
	if code, _ := call(t, a, "POST", "wake/"+d.ID, `{}`, "admin"); code != 200 {
		t.Fatal(code)
	}
	loaded, err := newApp(filepath.Dir(a.file))
	if err != nil || len(loaded.state.Tasks) != 1 || len(loaded.state.History) != 1 {
		t.Fatal(loaded, err)
	}
	if code, _ := call(t, a, "DELETE", "tasks/"+task.ID, "", "admin"); code != 200 {
		t.Fatal(code)
	}
	if len(a.state.History) != 1 {
		t.Fatal("task removal removed history")
	}
	if code, _ := call(t, a, "DELETE", "devices/"+d.ID, "", "admin"); code != 200 {
		t.Fatal(code)
	}
}

func TestScheduledSendsArePersistedBeforeTransmissionAndNotGloballyThrottled(t *testing.T) {
	a := fixture(t)
	first := add(t, a)
	code, data := call(t, a, "POST", "devices", `{"name":"设备 B","mac":"02:11:22:33:44:66","network":"auto"}`, "admin")
	if code != 200 {
		t.Fatal(code)
	}
	var second Device
	json.Unmarshal(data, &second)
	now := parsedTime(t, "2027-01-04T08:30:00Z")
	a.state.Tasks = []Task{taskFixture(first, now, "once"), taskFixture(second, now, "once")}
	if err := a.save(a.state); err != nil {
		t.Fatal(err)
	}
	sends := 0
	a.lastWake = now // Scheduled jobs for different devices bypass manual cooldown.
	a.send = func(packet []byte, n Network, port int) (int, error) {
		bytes, err := os.ReadFile(a.file)
		if err != nil {
			t.Fatal(err)
		}
		var disk State
		json.Unmarshal(bytes, &disk)
		task := disk.Tasks[sends]
		if task.Enabled || task.NextRun != nil || task.LastRun == nil || task.LastRun.Status != "running" {
			t.Fatal("not claimed before sending", task)
		}
		if len(packet) != 102 || port != 9 {
			t.Fatal("incorrect WOL path")
		}
		sends++
		return 3, nil
	}
	if err := a.schedulerTick(now); err != nil {
		t.Fatal(err)
	}
	if err := a.schedulerTick(now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if sends != 2 || len(a.state.History) != 2 {
		t.Fatal(sends, a.state.History)
	}
	for _, event := range a.state.History {
		if event.TaskID == "" || event.Actor == nil || event.Actor.ID != "100" || event.Sent != 3 {
			t.Fatal(event)
		}
	}
	loaded, err := newApp(filepath.Dir(a.file))
	if err != nil {
		t.Fatal(err)
	}
	loaded.send = func([]byte, Network, int) (int, error) { t.Error("duplicate after restart"); return 0, nil }
	if err := loaded.prepareTasks(now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := loaded.schedulerTick(now.Add(2 * time.Second)); err != nil {
		t.Fatal(err)
	}
}

func TestMissedTasksAndClockChangesDoNotCatchUp(t *testing.T) {
	a := fixture(t)
	d := add(t, a)
	now := parsedTime(t, "2027-01-04T08:30:00Z")
	due := now.Add(-10 * time.Second)
	once := taskFixture(d, due, "once")
	weekly := taskFixture(d, due, "weekdays")
	weekly.ID = "weekly"
	a.state.Tasks = []Task{once, weekly}
	a.save(a.state)
	a.send = func([]byte, Network, int) (int, error) { t.Error("missed job sent"); return 0, nil }
	if err := a.prepareTasks(now); err != nil {
		t.Fatal(err)
	}
	if a.state.Tasks[0].Enabled || a.state.Tasks[0].LastRun.Status != "missed" || a.state.Tasks[1].NextRun == nil || !a.state.Tasks[1].NextRun.After(now) {
		t.Fatal(a.state.Tasks)
	}
	if err := a.schedulerTick(now); err != nil {
		t.Fatal(err)
	}
	if err := a.schedulerTick(now.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	late := taskFixture(d, now.Add(-2*time.Minute), "once")
	a.state.Tasks = []Task{late}
	a.save(a.state)
	if err := a.schedulerTick(now); err != nil {
		t.Fatal(err)
	}
	if a.state.Tasks[0].LastRun.Status != "missed" || len(a.state.History) != 0 {
		t.Fatal(a.state)
	}
}

func TestSchedulePersistenceAndSendFailures(t *testing.T) {
	a := fixture(t)
	d := add(t, a)
	now := parsedTime(t, "2027-01-04T08:30:00Z")
	a.state.Tasks = []Task{taskFixture(d, now, "once")}
	a.save(a.state)
	original := a.file
	a.file = filepath.Join(a.file, "missing", "state.json")
	sends := 0
	a.send = func([]byte, Network, int) (int, error) { sends++; return 3, nil }
	if err := a.schedulerTick(now); err == nil || sends != 0 {
		t.Fatal("sent without persisting claim", err, sends)
	}
	a.file = original
	a.send = func([]byte, Network, int) (int, error) { sends++; return 0, errors.New("probe failure") }
	if err := a.schedulerTick(now); err != nil {
		t.Fatal(err)
	}
	if sends != 1 || a.state.Tasks[0].LastRun.Status != "failed" || a.state.History[0].Sent != 0 {
		t.Fatal(a.state)
	}
	// Simulate a disk failure only after the network operation has occurred.
	a.state.Tasks = []Task{taskFixture(d, now.Add(time.Minute), "once")}
	a.save(a.state)
	a.send = func([]byte, Network, int) (int, error) {
		sends++
		a.file = filepath.Join(original, "missing", "state.json")
		return 3, nil
	}
	if err := a.schedulerTick(now.Add(time.Minute)); err == nil {
		t.Fatal("missing result-write failure")
	}
	loaded, err := newApp(filepath.Dir(original))
	if err != nil {
		t.Fatal(err)
	}
	if loaded.state.Tasks[0].LastRun.Status != "running" {
		t.Fatal("claim was not durable")
	}
	if err := loaded.prepareTasks(now.Add(2 * time.Minute)); err != nil {
		t.Fatal(err)
	}
	if loaded.state.Tasks[0].Enabled || loaded.state.Tasks[0].LastRun.Status != "unknown" {
		t.Fatal(loaded.state.Tasks)
	}
}

func TestSchedulerStopsWithContext(t *testing.T) {
	a := fixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { a.runScheduler(ctx); close(done) }()
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("scheduler did not stop")
	}
}

func TestSchedulerRechecksClockBetweenJobs(t *testing.T) {
	a := fixture(t)
	d := add(t, a)
	now := parsedTime(t, "2027-01-04T08:30:00Z")
	first, second := taskFixture(d, now, "once"), taskFixture(d, now, "once")
	second.ID = "second"
	a.state.Tasks = []Task{first, second}
	a.save(a.state)
	sends := 0
	a.send = func([]byte, Network, int) (int, error) { sends++; now = now.Add(2 * time.Minute); return 3, nil }
	if err := a.schedulerTickWithClock(func() time.Time { return now }); err != nil {
		t.Fatal(err)
	}
	if sends != 1 || a.state.Tasks[1].LastRun.Status != "missed" || len(a.state.History) != 1 {
		t.Fatal(sends, a.state)
	}
}

// Opt-in NAS check: actual worker, persistence and UDP packets, all confined to
// loopback. It never broadcasts to the LAN or wakes a real target.
func TestSchedulerLoopbackOnNAS(t *testing.T) {
	if os.Getenv("WOL_SCHEDULER_INTEGRATION") != "1" {
		t.Skip("real NAS scheduler check is opt-in")
	}
	a := fixture(t)
	d := add(t, a)
	listener, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	a.send = func(packet []byte, n Network, port int) (int, error) {
		if port != 9 {
			t.Error(port)
		}
		return sendPacket(packet, Network{IP: "127.0.0.1", Broadcast: "127.0.0.1"}, listener.LocalAddr().(*net.UDPAddr).Port)
	}
	due := time.Now().Add(1500 * time.Millisecond)
	a.state.Tasks = []Task{taskFixture(d, due.UTC(), "once")}
	if err := a.save(a.state); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { a.runScheduler(ctx); close(done) }()
	expected, _, _ := magicPacket(d.MAC)
	listener.SetReadDeadline(time.Now().Add(8 * time.Second))
	for i := 0; i < 3; i++ {
		buffer := make([]byte, 512)
		n, _, err := listener.ReadFromUDP(buffer)
		if err != nil || !bytes.Equal(buffer[:n], expected) {
			t.Fatal("scheduled UDP payload", n, err)
		}
	}
	a.mu.Lock()
	sent := a.state.Tasks[0].LastRun != nil && a.state.Tasks[0].LastRun.Status == "sent" && len(a.state.History) == 1 && a.state.History[0].TaskID != ""
	a.mu.Unlock()
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("worker did not stop")
	}
	loaded, err := newApp(filepath.Dir(a.file))
	if err != nil || !sent || loaded.state.Tasks[0].Enabled || loaded.state.Tasks[0].LastRun.Sent != 3 {
		t.Fatal("worker/persistence failed", err)
	}
	t.Log("Unprivileged NAS scheduler delivered three verified loopback magic packets, persisted its result and stopped; no LAN broadcasts")
}
