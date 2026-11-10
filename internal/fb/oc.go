package fb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

type OC struct {
	CLI string // openclaw.mjs 路径（空 = 没找到）
}

func NewOC(p Paths) OC {
	if p.OC_CLI != "" {
		if _, err := os.Stat(p.OC_CLI); err == nil {
			return OC{CLI: p.OC_CLI}
		}
	}
	home, _ := os.UserHomeDir()
	var cands []string
	if runtime.GOOS == "windows" {
		cands = []string{filepath.Join(home, "AppData", "Roaming", "npm", "node_modules", "openclaw", "openclaw.mjs")}
	} else {
		cands = []string{"/usr/local/lib/node_modules/openclaw/openclaw.mjs", "/usr/lib/node_modules/openclaw/openclaw.mjs",
			filepath.Join(home, ".npm-global", "lib", "node_modules", "openclaw", "openclaw.mjs")}
	}
	for _, c := range cands {
		if _, err := os.Stat(c); err == nil {
			return OC{CLI: c}
		}
	}
	return OC{}
}

func (o OC) Available() bool { return o.CLI != "" }

func (o OC) call(ctx context.Context, method string, params map[string]any, timeout time.Duration) (map[string]any, error) {
	if o.CLI == "" {
		return nil, errors.New("openclaw command line not found (set OC_CLI in forgeboard.json)")
	}
	pb, _ := json.Marshal(params)
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(cctx, "node", o.CLI, "gateway", "call", method, "--params", string(pb))
	if runtime.GOOS == "windows" {
		cmd.SysProcAttr = hideCmd
	}
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("gateway call %s failed: %s%s", method, errText(err, ""), callErrDetail(err))
	}
	raw := string(out)
	i := strings.IndexByte(raw, '{')
	if i < 0 {
		return nil, fmt.Errorf("gateway call %s returned no JSON: %s", method, trunc(raw, 160))
	}
	var v map[string]any
	if err := json.Unmarshal([]byte(raw[i:]), &v); err != nil {
		return nil, fmt.Errorf("gateway call %s JSON parse failed: %w", method, err)
	}
	return v, nil
}

type Window struct {
	Key     string `json:"key"`
	Name    string `json:"name"`
	Where   string `json:"where"`
	Kind    string `json:"kind"` // chat | sys（sys = 定时任务/心跳等系统窗口，默认不动）
	Model   string `json:"model"`
	Used    int    `json:"used"`
	Cap     int    `json:"cap"`
	Running bool   `json:"running"`
}

type ModelCap struct {
	Ref string `json:"ref"`
	Cap int    `json:"cap"`
}

func ocName(s map[string]any) string {
	for _, k := range []string{"label", "subject", "displayName"} {
		if v, ok := s[k].(string); ok && v != "" {
			return trunc(v, 34)
		}
	}
	return trunc(str(s["key"]), 34)
}

func ocWhere(s map[string]any) string {
	c := str(s["channel"])
	if b, _ := s["isMain"].(bool); b {
		return "where-main"
	}
	switch c {
	case "feishu":
		if str(s["peerKind"]) == "group" {
			return "where-feishu-group"
		}
		return "where-feishu-direct"
	case "webchat":
		return "where-webchat"
	case "":
		return "where-other"
	}
	return "where-" + c
}

func ocClassify(s map[string]any) string {
	k := str(s["key"])
	if strings.Contains(k, ":cron:") {
		return "sys"
	}
	if b, _ := s["isMain"].(bool); b {
		return "chat"
	}
	if str(s["createdVia"]) == "channel" {
		return "chat"
	}
	nm := ocName(s)
	if strings.Contains(nm, "exec completion") || strings.Contains(nm, "心跳") {
		return "sys"
	}
	return "sys"
}

func (o OC) ListWindows(ctx context.Context) ([]Window, error) {
	v, err := o.call(ctx, "sessions.list", map[string]any{"limit": 100}, 60*time.Second)
	if err != nil {
		return nil, err
	}
	raw, _ := json.Marshal(v["sessions"])
	var rows []map[string]any
	if err := json.Unmarshal(raw, &rows); err != nil {
		return nil, fmt.Errorf("cannot parse sessions: %w", err)
	}
	var out []Window
	for _, s := range rows {
		if b, _ := s["archived"].(bool); b {
			continue
		}
		w := Window{Key: str(s["key"]), Name: ocName(s), Where: ocWhere(s), Kind: ocClassify(s), Model: str(s["model"])}
		if w.Model == "" {
			w.Model = str(s["activeModel"])
		}
		if n, ok := s["totalTokens"].(float64); ok {
			w.Used = int(n)
		}
		if n, ok := s["contextTokens"].(float64); ok {
			w.Cap = int(n)
		}
		if b, _ := s["hasActiveRun"].(bool); b {
			w.Running = true
		}
		out = append(out, w)
	}
	return out, nil
}

func (o OC) ModelCaps(ctx context.Context) ([]ModelCap, error) {
	v, err := o.call(ctx, "models.list", map[string]any{}, 60*time.Second)
	if err != nil {
		return nil, err
	}
	raw, _ := json.Marshal(v["models"])
	var rows []map[string]any
	if err := json.Unmarshal(raw, &rows); err != nil {
		return nil, fmt.Errorf("cannot parse models: %w", err)
	}
	var out []ModelCap
	for _, m := range rows {
		ref := str(m["id"])
		if ref == "" {
			continue
		}
		c := ModelCap{Ref: ref}
		if n, ok := m["contextWindow"].(float64); ok {
			c.Cap = int(n)
		} else if n, ok := m["contextTokens"].(float64); ok {
			c.Cap = int(n)
		}
		out = append(out, c)
	}
	return out, nil
}

func (o OC) PatchMany(ctx context.Context, model string, keys []string) error {
	if len(keys) == 0 {
		return errors.New("no window selected")
	}
	targets := make([]map[string]any, 0, len(keys))
	for _, k := range keys {
		targets = append(targets, map[string]any{"key": k})
	}
	_, err := o.call(ctx, "sessions.patchMany", map[string]any{
		"patch":   map[string]any{"model": model},
		"targets": targets,
	}, 120*time.Second)
	return err
}

type Cron struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Enabled  bool   `json:"enabled"`
	Schedule string `json:"schedule"`
	State    string `json:"state"`
	NextRun  int64  `json:"nextRun"`
	LastRun  int64  `json:"lastRun"`
	LastStat string `json:"lastStatus"`
	Kind     string `json:"kind,omitempty"`       // script | agentTurn | heartbeat（payload.kind）
	Model    string `json:"model,omitempty"`      // 定义里钉的模型（空＝跟默认）
	Needs    bool   `json:"needs"`                // 要不要模型：agentTurn/heartbeat 才要；纯脚本换了没意义
	RunModel string `json:"runModel,omitempty"`   // 最近一次真跑用的模型（cron.runs 现读；没钉时这才是真相）
	Launched string `json:"launchedBy,omitempty"` // 被哪条脚本任务手动拉起（脚本里 cron run <id>）：这种任务表上的时间不是它真正的触发时间
}

func (o OC) ListCrons(ctx context.Context) ([]Cron, error) {
	v, err := o.call(ctx, "cron.list", map[string]any{"limit": 100, "includeDisabled": true}, 60*time.Second)
	if err != nil {
		return nil, err
	}
	raw, _ := json.Marshal(v["jobs"])
	var rows []map[string]any
	if err := json.Unmarshal(raw, &rows); err != nil {
		return nil, fmt.Errorf("cannot parse cron jobs: %w", err)
	}
	var out []Cron
	var scripts []string // 脚本任务正文：判「这条其实是被脚本拉起来的」要用
	for _, r := range rows {
		c := Cron{ID: str(r["id"]), Name: trunc(firstNonEmpty(str(r["displayName"]), str(r["name"])), 60)}
		if b, ok := r["enabled"].(bool); ok {
			c.Enabled = b
		}
		c.Schedule = scheduleOf(r["schedule"])
		c.State = str(r["state"])
		if n, ok := r["nextRunAtMs"].(float64); ok {
			c.NextRun = int64(n)
		}
		if n, ok := r["lastRunAtMs"].(float64); ok {
			c.LastRun = int64(n)
		}
		c.LastStat = str(r["lastRunStatus"])
		var script string // 脚本任务的正文：后面判「这条任务是被脚本拉起来的」要用
		if pl, ok := r["payload"].(map[string]any); ok {
			c.Kind = str(pl["kind"])
			c.Model = str(pl["model"])
			script = str(pl["script"])
		}
		if c.Kind == "" {
			c.Kind = str(r["kind"])
		}
		c.Needs = c.Kind == "agentTurn" || c.Kind == "heartbeat"
		if c.Kind == "script" {
			scripts = append(scripts, script)
		}
		out = append(out, c)
	}
	for i := range out {
		if !out[i].Needs {
			continue
		}
		for _, sc := range scripts {
			if strings.Contains(sc, "cron run ") && strings.Contains(sc, out[i].ID) {
				out[i].Launched = out[i].Name
				break
			}
		}
	}
	return out, nil
}

func (o OC) RestartGateway(ctx context.Context, port int) (string, error) {
	if o.CLI == "" {
		return "", errors.New("openclaw command line not found (set OC_CLI in forgeboard.json)")
	}
	cctx, cancel := context.WithTimeout(ctx, 360*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, "node", o.CLI, "gateway", "restart")
	if runtime.GOOS == "windows" {
		cmd.SysProcAttr = hideCmd
	}
	out, err := cmd.CombinedOutput()
	txt := strings.TrimSpace(string(out))
	if err == nil {
		return txt, nil
	}
	if waitPortUp(port, 90) {
		return txt, nil // 命令没收尾，但端口已回话：重启其实成功了
	}
	return txt, fmt.Errorf("gateway restart failed: %s%s", errText(err, ""), outTail(txt))
}

// outTail 把子进程自己的回话附在错误后面：只报 exit status 1 看不出原因。
// callErrDetail 取 gateway call 失败时子进程写到 stderr 的原因行。
func callErrDetail(err error) string {
	if ee, ok := err.(*exec.ExitError); ok && len(ee.Stderr) > 0 {
		return "（" + tailText(strings.TrimSpace(string(ee.Stderr)), 240) + "）"
	}
	return ""
}

func outTail(txt string) string {
	if txt == "" {
		return ""
	}
	return "（命令回话：" + tailText(txt, 300) + "）"
}

func HeartbeatModels(p Paths) (heartbeat, compaction, primary string) {
	b, err := os.ReadFile(OCConfigPath(p))
	if err != nil {
		return "", "", ""
	}
	var j struct {
		Agents struct {
			Defaults struct {
				Heartbeat struct {
					Model string `json:"model"`
				} `json:"heartbeat"`
				Compaction struct {
					Model string `json:"model"`
				} `json:"compaction"`
				Model struct {
					Primary string `json:"primary"`
				} `json:"model"`
			} `json:"defaults"`
		} `json:"agents"`
	}
	if err := json.Unmarshal(b, &j); err != nil {
		return "", "", ""
	}
	return j.Agents.Defaults.Heartbeat.Model, j.Agents.Defaults.Compaction.Model, j.Agents.Defaults.Model.Primary
}

func scheduleOf(v any) string {
	switch s := v.(type) {
	case string:
		return trunc(s, 40)
	case map[string]any:
		if expr := firstNonEmpty(str(s["cron"]), str(s["expr"])); expr != "" {
			tz := str(s["tz"])
			if tz != "" {
				expr += " " + tz
			}
			return trunc(expr, 40)
		}
		if every := s["everySeconds"]; every != nil {
			if n, ok := every.(float64); ok {
				return fmt.Sprintf("every %ds", int(n))
			}
		}
		if every := s["everyMs"]; every != nil {
			if n, ok := every.(float64); ok && n >= 1000 {
				return fmt.Sprintf("every %ds", int(n/1000))
			}
		}
		if at := str(s["at"]); at != "" {
			return trunc(at, 40)
		}
		raw, _ := json.Marshal(s)
		return trunc(string(raw), 40)
	}
	return ""
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func str(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func trunc(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "..."
}

func (o OC) RunsLatest(ctx context.Context, ids []string) map[string]string {
	out := map[string]string{}
	if len(ids) == 0 {
		return out
	}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, id := range ids {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			v, err := o.call(ctx, "cron.runs", map[string]any{"jobId": id, "limit": 1}, 30*time.Second)
			if err != nil {
				return
			}
			raw, _ := json.Marshal(v["entries"])
			var rows []map[string]any
			if json.Unmarshal(raw, &rows) != nil || len(rows) == 0 {
				return
			}
			m := str(rows[0]["model"])
			if m == "" {
				return
			}
			if p := str(rows[0]["provider"]); p != "" {
				m = p + "/" + m
			}
			mu.Lock()
			out[id] = m
			mu.Unlock()
		}(id)
	}
	wg.Wait()
	return out
}
