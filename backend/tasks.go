package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	_ "time/tzdata"
)

type TaskInput struct {
	Name     string `json:"name"`
	DeviceID string `json:"device_id"`
	Repeat   string `json:"repeat"`
	Time     string `json:"time"`
	Date     string `json:"date,omitempty"`
	Weekdays []int  `json:"weekdays,omitempty"`
	Timezone string `json:"timezone"`
	Enabled  bool   `json:"enabled"`
}
type Task struct {
	TaskInput
	ID      string     `json:"id"`
	Author  User       `json:"author"`
	NextRun *time.Time `json:"next_run,omitempty"`
	LastRun *TaskRun   `json:"last_run,omitempty"`
}
type TaskRun struct {
	At      time.Time `json:"at"`
	DueAt   time.Time `json:"due_at"`
	Status  string    `json:"status"`
	Sent    int       `json:"sent"`
	Message string    `json:"message"`
}

// Resolve an IANA identifier when exposed by the NAS. The form falls back to
// the browser zone only when this is unavailable, and always saves the zone.
func defaultTimezone() string {
	values := []string{strings.TrimPrefix(os.Getenv("TZ"), ":")}
	if data, err := os.ReadFile("/etc/timezone"); err == nil {
		values = append(values, strings.TrimSpace(string(data)))
	}
	if link, err := filepath.EvalSymlinks("/etc/localtime"); err == nil {
		if _, zone, ok := strings.Cut(link, "/zoneinfo/"); ok {
			values = append(values, zone)
		}
	}
	for _, value := range values {
		if value == "" || value == "Local" || strings.HasPrefix(value, "/") {
			continue
		}
		if _, err := time.LoadLocation(value); err == nil {
			return value
		}
	}
	return ""
}

func validateTask(input *TaskInput) error {
	input.Name = strings.TrimSpace(input.Name)
	if len([]rune(input.Name)) > 40 {
		return errors.New("任务名称最多 40 个字符")
	}
	if len(input.Time) != 5 {
		return errors.New("请选择有效的小时和分钟")
	}
	if _, err := time.Parse("15:04", input.Time); err != nil {
		return errors.New("请选择有效的小时和分钟")
	}
	if input.Timezone == "" || input.Timezone == "Local" || len(input.Timezone) > 100 || strings.HasPrefix(input.Timezone, "/") {
		return errors.New("请选择有效的任务时区")
	}
	if _, err := time.LoadLocation(input.Timezone); err != nil {
		return errors.New("请选择有效的任务时区")
	}
	switch input.Repeat {
	case "once":
		if _, err := time.Parse("2006-01-02", input.Date); err != nil {
			return errors.New("请选择有效的执行日期")
		}
		input.Weekdays = nil
	case "daily":
		input.Weekdays = []int{1, 2, 3, 4, 5, 6, 7}
		input.Date = ""
	case "weekdays":
		input.Weekdays = []int{1, 2, 3, 4, 5}
		input.Date = ""
	case "weekends":
		input.Weekdays = []int{6, 7}
		input.Date = ""
	case "custom":
		if len(input.Weekdays) == 0 || len(input.Weekdays) > 7 {
			return errors.New("自定义重复至少选择一天")
		}
		seen := map[int]bool{}
		for _, day := range input.Weekdays {
			if day < 1 || day > 7 || seen[day] {
				return errors.New("星期选择无效或重复")
			}
			seen[day] = true
		}
		sort.Ints(input.Weekdays)
		input.Date = ""
	default:
		return errors.New("请选择有效的重复方式")
	}
	return nil
}

func taskOccurrence(input TaskInput, after time.Time) (*time.Time, error) {
	if err := validateTask(&input); err != nil {
		return nil, err
	}
	zone, _ := time.LoadLocation(input.Timezone)
	clock, _ := time.Parse("15:04", input.Time)
	local := after.In(zone)
	if input.Repeat == "once" {
		date, _ := time.Parse("2006-01-02", input.Date)
		candidate := time.Date(date.Year(), date.Month(), date.Day(), clock.Hour(), clock.Minute(), 0, 0, zone)
		if candidate.Format("2006-01-02 15:04") != input.Date+" "+input.Time {
			return nil, errors.New("该时间因时区切换不存在，请选择其他时间")
		}
		if !candidate.After(after) {
			return nil, nil
		}
		utc := candidate.UTC()
		return &utc, nil
	}
	allowed := map[int]bool{}
	for _, day := range input.Weekdays {
		allowed[day] = true
	}
	// Fourteen calendar days also cover a skipped local time at a DST change.
	for offset := 0; offset < 15; offset++ {
		date := time.Date(local.Year(), local.Month(), local.Day()+offset, 12, 0, 0, 0, zone)
		day := int(date.Weekday())
		if day == 0 {
			day = 7
		}
		if !allowed[day] {
			continue
		}
		candidate := time.Date(date.Year(), date.Month(), date.Day(), clock.Hour(), clock.Minute(), 0, 0, zone)
		if candidate.Year() != date.Year() || candidate.YearDay() != date.YearDay() || candidate.Hour() != clock.Hour() || candidate.Minute() != clock.Minute() {
			continue
		}
		if candidate.After(after) {
			utc := candidate.UTC()
			return &utc, nil
		}
	}
	return nil, errors.New("无法计算下次执行时间，请检查时间和时区")
}

func (a *App) writeTask(w http.ResponseWriter, r *http.Request, id string) {
	var input TaskInput
	if !decode(w, r, &input) {
		return
	}
	if err := validateTask(&input); err != nil {
		problem(w, 400, err.Error())
		return
	}
	nextRun, err := taskOccurrence(input, time.Now())
	if err != nil {
		problem(w, 400, err.Error())
		return
	}
	if input.Enabled && nextRun == nil {
		problem(w, 400, "一次性任务的时间需晚于当前 NAS 时间")
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	found := false
	for _, d := range a.state.Devices {
		if d.ID == input.DeviceID {
			found = true
			if input.Name == "" {
				input.Name = string([]rune(d.Name + "定时唤醒")[:min(40, len([]rune(d.Name+"定时唤醒")))])
			}
			break
		}
	}
	if !found {
		problem(w, 400, "请选择已保存的设备")
		return
	}
	next := a.state
	next.Tasks = append([]Task{}, a.state.Tasks...)
	index := -1
	for i, task := range next.Tasks {
		if task.ID == id {
			index = i
			break
		}
	}
	if id != "" && index < 0 {
		problem(w, 404, "定时任务不存在")
		return
	}
	if id == "" {
		if len(next.Tasks) >= 64 {
			problem(w, 400, "最多可保存 64 个定时任务")
			return
		}
		var bytes [16]byte
		if _, err := rand.Read(bytes[:]); err != nil {
			problem(w, 500, "无法生成任务标识")
			return
		}
		id = hex.EncodeToString(bytes[:])
	}
	task := Task{TaskInput: input, ID: id, Author: r.Context().Value(userContextKey{}).(User)}
	if input.Enabled {
		task.NextRun = nextRun
	}
	if index >= 0 {
		task.LastRun = next.Tasks[index].LastRun
		next.Tasks[index] = task
	} else {
		next.Tasks = append(next.Tasks, task)
	}
	if err := a.save(next); err != nil {
		problem(w, 500, "任务保存失败，请检查数据目录权限和剩余空间")
		return
	}
	respond(w, 200, task)
}

func (a *App) taskRoute(w http.ResponseWriter, r *http.Request, id string) {
	if id == "" || strings.Contains(id, "/") {
		problem(w, 404, "定时任务不存在")
		return
	}
	if r.Method == "PUT" {
		a.writeTask(w, r, id)
		return
	}
	if r.Method != "DELETE" {
		problem(w, 405, "请求方法不支持")
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	next := a.state
	next.Tasks = []Task{}
	found := false
	for _, task := range a.state.Tasks {
		if task.ID == id {
			found = true
		} else {
			next.Tasks = append(next.Tasks, task)
		}
	}
	if !found {
		problem(w, 404, "定时任务不存在")
		return
	}
	if err := a.save(next); err != nil {
		problem(w, 500, "任务删除失败，请检查数据目录")
		return
	}
	respond(w, 200, map[string]bool{"ok": true})
}

// On startup, skip all elapsed occurrences. Claimed jobs are never retried:
// a crash after network transmission cannot tell whether the packet arrived.
func (a *App) prepareTasks(now time.Time) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	next := a.state
	next.Tasks = append([]Task{}, a.state.Tasks...)
	changed := false
	for i := range next.Tasks {
		task := &next.Tasks[i]
		input := task.TaskInput
		if err := validateTask(&input); err != nil {
			task.Enabled = false
			task.NextRun = nil
			task.LastRun = &TaskRun{At: now.UTC(), Status: "failed", Message: err.Error()}
			changed = true
			continue
		}
		if task.LastRun != nil && task.LastRun.Status == "running" {
			run := *task.LastRun
			run.Status = "unknown"
			run.Message = "上次执行被中断，结果未能确认；不会重复补发"
			task.LastRun = &run
			changed = true
		}
		if !task.Enabled {
			if task.NextRun != nil {
				task.NextRun = nil
				changed = true
			}
			continue
		}
		if task.NextRun != nil && task.NextRun.After(now) {
			continue
		}
		due := now
		if task.NextRun != nil {
			due = *task.NextRun
		}
		following, err := taskOccurrence(task.TaskInput, now)
		task.NextRun = following
		if err != nil || following == nil {
			task.Enabled = false
		}
		message := "NAS 或应用未运行，已跳过错过的执行时间"
		if err != nil {
			message = err.Error()
		}
		task.LastRun = &TaskRun{At: now.UTC(), DueAt: due, Status: "missed", Message: message}
		changed = true
	}
	if changed {
		if err := a.save(next); err != nil {
			return err
		}
	}
	return nil
}

func (a *App) schedulerTick(now time.Time) error {
	return a.schedulerTickWithClock(func() time.Time { return now })
}

func (a *App) schedulerTickWithClock(clock func() time.Time) error {
	a.mu.Lock()
	ids := make([]string, 0, len(a.state.Tasks))
	for _, task := range a.state.Tasks {
		ids = append(ids, task.ID)
	}
	a.mu.Unlock()
	for _, id := range ids {
		if err := a.executeTask(id, clock); err != nil {
			return err
		}
	}
	return nil
}

// Release the state lock between jobs so a busy scheduled minute does not
// monopolize device management. Edits and deletes are rechecked for each ID.
func (a *App) executeTask(id string, clock func() time.Time) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	for i := range a.state.Tasks {
		now := clock()
		task := a.state.Tasks[i]
		if task.ID != id {
			continue
		}
		if !task.Enabled || task.NextRun == nil || task.NextRun.After(now) {
			continue
		}
		due := *task.NextRun
		following, err := taskOccurrence(task.TaskInput, now)
		if err != nil {
			return err
		}
		task.NextRun = following
		if task.Repeat == "once" || following == nil {
			task.Enabled = false
		}
		task.LastRun = &TaskRun{At: now.UTC(), DueAt: due, Status: "running", Message: "正在发送唤醒包"}
		if now.Sub(due) >= time.Minute {
			task.LastRun.Status = "missed"
			task.LastRun.Message = "执行时间已错过，已跳过本次任务"
		}
		next := a.state
		next.Tasks = append([]Task{}, a.state.Tasks...)
		next.Tasks[i] = task
		// Persist consumption BEFORE any packet is sent. Persistence failure
		// leaves the occurrence pending and sends nothing.
		if err := a.save(next); err != nil {
			return fmt.Errorf("无法保存任务执行状态，未发送唤醒包: %w", err)
		}
		if task.LastRun.Status == "missed" {
			continue
		}
		var device Device
		for _, d := range a.state.Devices {
			if d.ID == task.DeviceID {
				device = d
				break
			}
		}
		var event Event
		if device.ID == "" {
			event = Event{Time: now.UTC(), Name: task.Name, Targets: []string{}, Actor: &task.Author, Error: "任务设备已不存在"}
		} else {
			var failure *wakeFailure
			event, failure = a.transmitWake(device, task.Author, now)
			if failure != nil {
				event.Error = failure.Message
			}
		}
		event.TaskID, event.TaskName = task.ID, task.Name
		status, message := "sent", "唤醒包已发送"
		if event.Sent == 0 {
			status, message = "failed", event.Error
		} else if event.Error != "" {
			status, message = "partial", event.Error
		}
		if message == "" {
			message = "唤醒包发送失败"
		}
		task.LastRun = &TaskRun{At: now.UTC(), DueAt: due, Status: status, Sent: event.Sent, Message: message}
		next = a.state
		next.Tasks = append([]Task{}, a.state.Tasks...)
		next.Tasks[i] = task
		next.History = appendHistory(event, a.state.History)
		if err := a.save(next); err != nil {
			a.state = next
			return fmt.Errorf("任务已执行，但结果保存失败: %w", err)
		}
	}
	return nil
}

func (a *App) runScheduler(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	ready := false
	for {
		if ctx.Err() != nil {
			return
		}
		now := time.Now()
		var err error
		if !ready {
			err = a.prepareTasks(now)
			ready = err == nil
		}
		if err == nil {
			err = a.schedulerTickWithClock(time.Now)
		}
		message := ""
		if err != nil {
			message = err.Error()
		}
		a.mu.Lock()
		previous := a.taskError
		if message != "" {
			a.taskError = message
		}
		a.mu.Unlock()
		if message != "" && message != previous {
			log.Printf("scheduler: %s", message)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
