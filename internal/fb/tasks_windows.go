//go:build windows

package fb

//

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"
)

type TaskItem struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	System  bool   `json:"system"`  // 住在 \Microsoft\... 那批系统文件夹里
	Hidden  bool   `json:"hidden"`  // 任务计划程序里勾了「隐藏」
	Enabled bool   `json:"enabled"` // 任务自己有没有被禁用
	State   string `json:"state"`   // running | ready | queued | disabled | unknown
	Trigger string `json:"trigger"` // 触发方式（开机/登录/会话…），机器名原样给，界面翻译
	LastRun int64  `json:"lastRun"`
	Result  string `json:"result"` // ok | running | notrun | noschedule | failed | unknown
	ResultNote string `json:"resultNote,omitempty"` // 失败时带上原始码
}

const (
	suTaskDisabled = 1
	suTaskQueued   = 2
	suTaskReady    = 3
	suTaskRunning  = 4
)

const suTaskQuery = `$ErrorActionPreference='Continue'
[Console]::OutputEncoding=[Text.UTF8Encoding]::new($false)
Get-ScheduledTask | ForEach-Object {
  $d = $_
  $trig = ($d.Triggers | ForEach-Object { $_.CimClass.CimClassName -replace 'MSFT_Task','' }) -join ','
  if ($trig -notmatch 'Boot|Logon|SessionState' -and $d.TaskPath -ne '\') { return }
  $d | Get-ScheduledTaskInfo -ErrorAction SilentlyContinue | ForEach-Object {
    [pscustomobject]@{
      name    = [string]$d.TaskName
      path    = [string]$d.TaskPath
      state   = [string]$d.State
      enabled = [string]$d.Settings.Enabled
      hidden  = [string]$d.Settings.Hidden
      trig    = [string]$trig
      last    = [string]$_.LastRunTime
      result  = [string]$_.LastTaskResult
    }
  }
} | ConvertTo-Json -Compress
`

type suTaskRow struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	State   string `json:"state"`
	Enabled string `json:"enabled"`
	Hidden  string `json:"hidden"`
	Trig    string `json:"trig"`
	Last    string `json:"last"`
	Result  string `json:"result"`
}

func suInt(s string) (int64, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

func suTaskState(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "running", "4":
		return "running"
	case "ready", "3":
		return "ready"
	case "queued", "2":
		return "queued"
	case "disabled", "1":
		return "disabled"
	}
	return "unknown"
}

func suBool(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "true", "1", "yes":
		return true
	}
	return false
}

func suTaskTime(s string) int64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	for _, f := range []string{"01/02/2006 15:04:05", "1/2/2006 15:04:05", "01/02/2006 3:04:05 PM", "1/2/2006 3:04:05 PM"} {
		if t, err := time.ParseInLocation(f, s, time.Local); err == nil {
			if t.Year() < 2000 {
				return 0
			}
			return t.UnixMilli()
		}
	}
	return 0
}

func suResult(code int64, ok bool) (string, string) {
	if !ok {
		return "unknown", ""
	}
	switch code {
	case 0:
		return "ok", ""
	case 0x41301:
		return "running", ""
	case 0x41306:
		return "notrun", ""
	case 0x41303:
		return "noschedule", ""
	case 0x41308:
		return "manual", ""
	case 0x41320:
		return "recurrence", ""
	default:
		return "failed", fmt.Sprintf("0x%X", code)
	}
}

func TaskList(ctx context.Context) ([]TaskItem, error) {
	cctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, "pwsh", "-NoProfile", "-NonInteractive", "-Command", suTaskQuery)
	cmd.SysProcAttr = hideCmd
	out, err := cmd.Output()
	if err != nil {
		detail := ""
		if ee, ok := err.(*exec.ExitError); ok {
			detail = strings.TrimSpace(string(ee.Stderr))
		}
		if detail != "" {
			return nil, fmt.Errorf("cannot read scheduled tasks: %w (%s)", err, trunc(detail, 200))
		}
		return nil, fmt.Errorf("cannot read scheduled tasks: %w", err)
	}
	raw := strings.TrimSpace(string(out))
	if raw == "" {
		return nil, errors.New("scheduled tasks came back empty")
	}
	if !strings.HasPrefix(raw, "[") && !strings.HasPrefix(raw, "{") {
		return nil, fmt.Errorf("scheduled tasks unreadable: %s", trunc(raw, 160))
	}
	var rows []suTaskRow
	if len(raw) > 0 && raw[0] == '{' { // 只剩一条时 ConvertTo-Json 不给数组
		var one suTaskRow
		if json.Unmarshal([]byte(raw), &one) != nil {
			return nil, errors.New("scheduled tasks JSON parse failed")
		}
		rows = append(rows, one)
	} else if json.Unmarshal([]byte(raw), &rows) != nil {
		return nil, errors.New("scheduled tasks JSON parse failed")
	}
	var out2 []TaskItem
	for _, r := range rows {
		st := suTaskState(r.State)
		enabled := suBool(r.Enabled)
		if !enabled {
			st = "disabled" // 任务自己被禁用了，状态就是「已禁用」
		}
		code, codeOK := suInt(r.Result)
		res, note := suResult(code, codeOK)
		out2 = append(out2, TaskItem{
			Name: r.Name, Path: r.Path,
			System: strings.HasPrefix(strings.ToUpper(r.Path), `\MICROSOFT\`),
			Hidden: suBool(r.Hidden), Enabled: enabled, State: st, Trigger: r.Trig,
			LastRun: suTaskTime(r.Last), Result: res, ResultNote: note,
		})
	}
	sort.Slice(out2, func(i, j int) bool {
		ir, jr := out2[i].Path == `\`, out2[j].Path == `\`
		if ir != jr {
			return ir
		}
		if out2[i].Name != out2[j].Name {
			return out2[i].Name < out2[j].Name
		}
		return out2[i].Path < out2[j].Path
	})
	return out2, nil
}
