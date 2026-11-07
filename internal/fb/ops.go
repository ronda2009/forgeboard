package fb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"
)

var reReserveFirst = regexp.MustCompile(`"--vram-reserve-mib",\s*"(\d+)"`)
var reReserveLater = regexp.MustCompile(`"--vram-reserve-later-mib",\s*"(\d+)"`)

type ReserveRow struct {
	File  string `json:"file"`
	First int    `json:"first"`
	Later int    `json:"later"`
}

func tierFiles(p Paths) []string {
	if p.TIER_DIR == "" {
		return nil
	}
	var out []string
	seen := map[string]bool{}
	for _, dir := range []string{p.TIER_DIR, tierRoot(p)} {
		if dir == "" {
			continue
		}
		fs_, _ := filepath.Glob(filepath.Join(dir, "strata-*.json"))
		for _, f := range fs_ {
			if seen[f] {
				continue
			}
			b, err := os.ReadFile(f)
			if err != nil || !reModelName.Match(b) {
				continue // 没有 model_name 就不是档位文件（状态文件、检查记录）
			}
			seen[f] = true
			out = append(out, f)
		}
	}
	sort.Strings(out)
	return out
}

func ReserveNow(p Paths) ([]ReserveRow, error) {
	files := tierFiles(p)
	if len(files) == 0 {
		return nil, fmt.Errorf("no tier files (strata-*.json with model_name) found under %s or %s", p.TIER_DIR, tierRoot(p))
	}
	var out []ReserveRow
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		t := string(b)
		r := ReserveRow{File: filepath.Base(f)}
		if m := reReserveFirst.FindStringSubmatch(t); m != nil {
			r.First = atoi(m[1])
		}
		if m := reReserveLater.FindStringSubmatch(t); m != nil {
			r.Later = atoi(m[1])
		}
		out = append(out, r)
	}
	return out, nil
}

func ReserveSet(ctx context.Context, p Paths, first, later int, dry bool) (*OpResult, error) {
	files := tierFiles(p)
	if len(files) == 0 {
		return nil, fmt.Errorf("no tier files (strata-*.json with model_name) found under %s or %s", p.TIER_DIR, tierRoot(p))
	}
	if dry {
		rows, err := ReserveNow(p)
		if err != nil {
			return nil, err
		}
		res := op("reserve_dry", true).subj("count", itoa(len(files)), "first", itoa(first), "later", itoa(later))
		res.Rows = rows
		return res, nil
	}
	if first <= 0 {
		return nil, errors.New("no reserve value given")
	}
	bak := filepath.Join(p.TIER_DIR, ".reserve-bak")
	if err := os.MkdirAll(bak, 0o755); err != nil {
		return nil, err
	}
	stamp := time.Now().Format("20060102-150405")
	changed := 0
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		t := string(b)
		_ = os.WriteFile(filepath.Join(bak, filepath.Base(f)+"."+stamp), b, 0o644)
		t2 := reReserveFirst.ReplaceAllString(t, `"--vram-reserve-mib", "`+itoa(first)+`"`)
		t2 = reReserveLater.ReplaceAllString(t2, `"--vram-reserve-later-mib", "`+itoa(later)+`"`)
		if t2 != t {
			if err := os.WriteFile(f, []byte(t2), 0o644); err != nil {
				return nil, err
			}
			changed++
		}
	}
	rows, err := ReserveNow(p)
	if err != nil {
		return nil, err
	}
	var bad []string
	for _, r := range rows {
		if r.First != first || r.Later != later {
			bad = append(bad, fmt.Sprintf("%s reads %d/%d", r.File, r.First, r.Later))
		}
	}
	res := op("reserve_done", false).subj("count", itoa(changed), "first", itoa(first), "later", itoa(later), "stamp", stamp)
	if len(bad) > 0 {
		res.Token = "reserve_mismatch"
		res.Bad = bad
	}
	return res, nil
}

func ReserveRollback(ctx context.Context, p Paths, dry bool) (*OpResult, error) {
	files := tierFiles(p)
	if len(files) == 0 {
		return nil, fmt.Errorf("no tier files (strata-*.json with model_name) found under %s or %s", p.TIER_DIR, tierRoot(p))
	}
	bak := filepath.Join(p.TIER_DIR, ".reserve-bak")
	bks, _ := filepath.Glob(filepath.Join(bak, "strata-*.json.*"))
	if len(bks) == 0 {
		return nil, errors.New("no backups in .reserve-bak - nothing to restore")
	}
	sort.Slice(bks, func(i, j int) bool {
		a, _ := os.Stat(bks[i])
		b, _ := os.Stat(bks[j])
		if a == nil || b == nil {
			return bks[i] < bks[j]
		}
		return a.ModTime().Before(b.ModTime())
	})
	stamp := ""
	if m := regexp.MustCompile(`(\d{8}-\d{6})$`).FindString(bks[len(bks)-1]); m != "" {
		stamp = m
	}
	if stamp == "" {
		return nil, errors.New("backup file names carry no timestamp - cannot tell which batch is newest")
	}
	var first, later = -1, -1
	var sample string
	for _, b := range bks {
		if !strings.HasSuffix(b, "."+stamp) {
			continue
		}
		t, err := os.ReadFile(b)
		if err != nil {
			continue
		}
		if sample == "" {
			sample = filepath.Base(b)
		}
		if m := reReserveFirst.FindStringSubmatch(string(t)); m != nil {
			first = atoi(m[1])
		}
		if m := reReserveLater.FindStringSubmatch(string(t)); m != nil {
			later = atoi(m[1])
		}
	}
	if first < 0 {
		return nil, fmt.Errorf("cannot read reserve values from backup batch %s", stamp)
	}
	if dry {
		return op("reserve_rb_dry", true).subj("stamp", stamp, "sample", sample, "first", itoa(first), "later", itoa(later)), nil
	}
	n := 0
	for _, b := range bks {
		if !strings.HasSuffix(b, "."+stamp) {
			continue
		}
		name := strings.TrimSuffix(filepath.Base(b), "."+stamp)
		t, err := os.ReadFile(b)
		if err != nil {
			continue
		}
		if err := os.WriteFile(filepath.Join(p.TIER_DIR, name), t, 0o644); err != nil {
			return nil, err
		}
		n++
	}
	rows, err := ReserveNow(p)
	if err != nil {
		return nil, err
	}
	var bad []string
	for _, r := range rows {
		if r.First != first || r.Later != later {
			bad = append(bad, fmt.Sprintf("%s reads %d/%d", r.File, r.First, r.Later))
		}
	}
	res := op("reserve_rb_done", false).subj("count", itoa(n), "stamp", stamp, "first", itoa(first), "later", itoa(later))
	if len(bad) > 0 {
		res.Token = "reserve_mismatch"
		res.Bad = bad
	}
	return res, nil
}

type CacheInfo struct {
	Enabled  bool   `json:"enabled"`
	Slots    int    `json:"slots"`
	Parked   int    `json:"parked"`
	Bytes    int64  `json:"bytes"`
	Reused   int64  `json:"reused_tokens"`
	HeldTok  int    `json:"held_tokens"` // /slots[0].n_prompt_tokens（引擎里停靠的提示词长度）
	Tier     string `json:"tier"`
	Readable bool   `json:"readable"`
	Note     string `json:"note,omitempty"`
}

func routerKey(p Paths) string {
	home := p.OC_HOME
	if home == "" {
		h, _ := os.UserHomeDir()
		home = filepath.Join(h, ".openclaw")
	}
	k, err := os.ReadFile(filepath.Join(home, "secrets", "strata.key"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(k))
}

func CacheStatus(ctx context.Context, p Paths) CacheInfo {
	cctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	var ci CacheInfo
	key := routerKey(p)
	get := func(path string) ([]byte, bool) {
		req, err := http.NewRequestWithContext(cctx, http.MethodGet, p.RouterURL+path, nil)
		if err != nil {
			return nil, false
		}
		if key != "" {
			req.Header.Set("Authorization", "Bearer "+key)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, false
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		if resp.StatusCode != 200 {
			return b, false
		}
		return b, true
	}
	if b, ok := get("/__status"); ok {
		var s struct {
			Current string `json:"current"`
		}
		if json.Unmarshal(b, &s) == nil {
			ci.Tier = s.Current
		}
	}
	b, ok := get("/metrics")
	if !ok {
		ci.Note = "note-cache-unreadable"
		return ci
	}
	var m struct {
		Cache struct {
			Enabled      bool  `json:"enabled"`
			Slots        int   `json:"slots"`
			Parked       int   `json:"parked"`
			Bytes        int64 `json:"bytes"`
			ReusedTokens int64 `json:"reused_tokens"`
		} `json:"conversation_cache"`
	}
	if json.Unmarshal(b, &m) != nil {
		ci.Note = "note-cache-shape"
		return ci
	}
	ci.Enabled, ci.Slots, ci.Parked, ci.Bytes, ci.Reused = m.Cache.Enabled, m.Cache.Slots, m.Cache.Parked, m.Cache.Bytes, m.Cache.ReusedTokens
	if sb, ok2 := get("/slots"); ok2 {
		var sl []struct {
			NPromptTokens int `json:"n_prompt_tokens"`
		}
		if json.Unmarshal(sb, &sl) == nil && len(sl) > 0 {
			ci.HeldTok = sl[0].NPromptTokens
		}
	}
	ci.Readable = true
	return ci
}

func ClearCache(ctx context.Context, p Paths, dry bool) (*OpResult, error) {
	ci := CacheStatus(ctx, p)
	token := "cache_done"
	if dry {
		token = "cache_dry"
	}
	res := op(token, dry).subj(
		"tier", orNone(ci.Tier),
		"parked", itoa(ci.Parked),
		"slots", itoa(ci.Slots),
		"gib", fmt.Sprintf("%.2f", float64(ci.Bytes)/(1<<30)),
		"tokens", itoa(ci.HeldTok),
	)
	if !ci.Readable {
		res.Token = "cache_unreadable"
		res.Note = ci.Note
	}
	if dry {
		return res, nil
	}
	key := routerKey(p)
	post := func(path string, timeout time.Duration) (string, error) {
		cctx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		req, err := http.NewRequestWithContext(cctx, http.MethodPost, p.RouterURL+path, strings.NewReader("{}"))
		if err != nil {
			return "", err
		}
		req.Header.Set("Content-Type", "application/json")
		if key != "" {
			req.Header.Set("Authorization", "Bearer "+key)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return "", err
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return string(b), nil
	}
	Step("step_cache_unload", "tier", orNone(ci.Tier))
	r, err := post("/v1/unload?wait=1", 320*time.Second)
	if err != nil {
		return nil, fmt.Errorf("unload failed: %w (HTTP 409 means busy, try again shortly)", err)
	}
	if s := strings.TrimSpace(r); s != "" {
		res.Note = "unload: " + trunc(s, 160)
	}
	Step("step_cache_load", "tier", orNone(ci.Tier))
	if _, err := post("/v1/load", 320*time.Second); err != nil {
		res.Note = strings.TrimSpace(res.Note+" | load: "+trunc(err.Error(), 160))
	}
	model := ci.Tier
	if model == "" {
		model = "qwen3.8-flash-next"
	}
	Step("step_cache_wait", "tier", model)
	ready := false
	for i := 0; i < 240; i++ {
		select {
		case <-ctx.Done():
			res.Token = "cache_cancelled"
			return res, nil
		case <-time.After(time.Second):
		}
		if bigPing(ctx, p.RouterURL, model, key) {
			ready = true
			break
		}
	}
	after := CacheStatus(ctx, p)
	res.subj("after_parked", itoa(after.Parked), "after_slots", itoa(after.Slots),
		"after_gib", fmt.Sprintf("%.2f", float64(after.Bytes)/(1<<30)))
	switch {
	case !ready:
		res.Token = "cache_notready"
	case ci.Readable && after.Readable:
		res.Token = "cache_done"
	default:
		res.Token = "cache_done_noafter"
	}
	return res, nil
}

func orNone(s string) string {
	if s == "" {
		return "unknown"
	}
	return s
}

func bigPing(ctx context.Context, base, model, key string) bool {
	cctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	body := `{"model":"` + model + `","messages":[{"role":"user","content":"ping"}],"max_tokens":1}`
	req, err := http.NewRequestWithContext(cctx, http.MethodPost, base+"/v1/chat/completions", strings.NewReader(body))
	if err != nil {
		return false
	}
	req.Header.Set("Content-Type", "application/json")
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
	return resp.StatusCode == 200
}

func RestartBig(ctx context.Context, p Paths, dry bool) (*OpResult, error) {
	rs := AskRouter(ctx, p.RouterURL)
	if rs == nil {
		return nil, fmt.Errorf("router %s is not answering, so the current tier is unknown", p.RouterURL)
	}
	tier := rs.Current
	if tier == "" {
		return nil, errors.New("the router reports no current tier (it may be switching)")
	}
	var ports []int
	for _, pt := range rs.Tiers {
		if pt > 0 && PortUp(pt) {
			ports = append(ports, pt)
		}
	}
	if dry {
		var ps []string
		for _, pt := range ports {
			ps = append(ps, itoa(pt))
		}
		return op("restart_big_dry", true).subj("ports", joinOrNone(ps), "url", p.RouterURL, "tier", tier), nil
	}
	Step("step_big_kill", "tier", tier, "ports", joinOrNone(portList(ports)))
	for _, pt := range ports {
		_ = killOnPort(pt)
	}
	time.Sleep(2 * time.Second)
	Step("step_big_switch", "tier", tier)
	r, err := getJSON(ctx, p.RouterURL+"/__switch?tier="+tier)
	if err != nil {
		return nil, fmt.Errorf("router not answering: %w", err)
	}
	if r.code != 200 {
		return nil, fmt.Errorf("__switch returned HTTP %d: %s", r.code, strings.TrimSpace(string(r.body)))
	}
	res := op("restart_big_done", false).subj("tier", tier)
	Step("step_big_wait", "tier", tier)
	if !waitRouterTier(ctx, p.RouterURL, tier) {
		Step("step_big_slow", "tier", tier)
		res.Token = "restart_big_notready"
	}
	return res, nil
}

func portList(ports []int) []string {
	out := make([]string, 0, len(ports))
	for _, pt := range ports {
		out = append(out, itoa(pt))
	}
	return out
}

func joinOrNone(xs []string) string {
	if len(xs) == 0 {
		return "none"
	}
	return strings.Join(xs, ", ")
}

func RescueGateway(ctx context.Context, p Paths, hard, dry bool) (*OpResult, error) {
	if p.GW_RESCUE == "" {
		return nil, errors.New("no rescue script configured (set GW_RESCUE in forgeboard.json)")
	}
	if _, err := os.Stat(p.GW_RESCUE); err != nil {
		return nil, fmt.Errorf("cannot find %s", p.GW_RESCUE)
	}
	var name string
	var args []string
	switch strings.ToLower(filepath.Ext(p.GW_RESCUE)) {
	case ".sh":
		name = "bash"
		if _, err := exec.LookPath("bash"); err != nil {
			name = "sh"
		}
		args = []string{p.GW_RESCUE}
		if hard {
			args = append(args, "-Hard")
		}
		if dry {
			args = append(args, "-DryRun")
		}
	default: // .ps1 与没后缀：当 PowerShell 脚本跑
		spath := strings.ReplaceAll(p.GW_RESCUE, "'", "''")
		run := "[Console]::OutputEncoding=[System.Text.Encoding]::UTF8; & '" + spath + "'"
		if hard {
			run += " -Hard"
		}
		if dry {
			run += " -DryRun"
		}
		name = "pwsh"
		args = []string{"-NoProfile", "-ExecutionPolicy", "Bypass", "-Command", run}
		if _, err := exec.LookPath("pwsh"); err != nil {
			name = "powershell"
		}
	}
	cctx, cancel := context.WithTimeout(ctx, 240*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, name, args...)
	if runtime.GOOS == "windows" {
		cmd.SysProcAttr = hideCmd
	}
	out, err := cmd.CombinedOutput()
	txt := strings.TrimSpace(string(out))
	if err != nil {
		if txt == "" {
			return nil, fmt.Errorf("cannot run %s: %w (not installed or wrong path)", name, err)
		}
		return nil, fmt.Errorf("%s", errText(err, txt))
	}
	token := "rescue_done"
	if dry {
		token = "rescue_dry"
	}
	return op(token, dry).subj("script", filepath.Base(p.GW_RESCUE)).withNote(txt), nil
}

func SetHeartbeat(p Paths, model string, dry bool) (*OpResult, error) {
	cfg := OCConfigPath(p)
	if cfg == "" {
		return nil, errors.New("cannot find openclaw.json")
	}
	if model == "" {
		return nil, errors.New("no target model given")
	}
	b, err := os.ReadFile(cfg)
	if err != nil {
		return nil, err
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
			} `json:"defaults"`
		} `json:"agents"`
	}
	if json.Unmarshal(b, &j) != nil {
		return nil, fmt.Errorf("cannot parse %s", filepath.Base(cfg))
	}
	hb, cp := j.Agents.Defaults.Heartbeat.Model, j.Agents.Defaults.Compaction.Model
	if hb == "" && cp == "" {
		return nil, errors.New("openclaw.json has neither heartbeat nor compaction - refusing to invent them")
	}
	if dry {
		return op("hb_dry", true).subj("model", model, "hb", orNone(hb), "cp", orNone(cp)), nil
	}
	stamp := time.Now().Format("20060102-150405")
	if err := os.WriteFile(cfg+".hb-bak-"+stamp, b, 0o644); err != nil {
		return nil, err
	}
	t := string(b)
	n := 0
	if hb != "" {
		re := regexp.MustCompile(`("heartbeat"\s*:\s*\{[^{}]*?"model"\s*:\s*")[^"]*(")`)
		t2 := re.ReplaceAllString(t, "$1"+model+"$2")
		if t2 != t {
			t = t2
			n++
		}
	}
	if cp != "" {
		re := regexp.MustCompile(`("compaction"\s*:\s*\{[^{}]*?"model"\s*:\s*")[^"]*(")`)
		t2 := re.ReplaceAllString(t, "$1"+model+"$2")
		if t2 != t {
			t = t2
			n++
		}
	}
	if n == 0 {
		return nil, fmt.Errorf("no writable location found (backup kept at openclaw.json.hb-bak-%s, file untouched)", stamp)
	}
	if err := os.WriteFile(cfg, []byte(t), 0o644); err != nil {
		return nil, err
	}
	nh, nc, _ := HeartbeatModels(p)
	res := op("hb_done", false).subj(
		"count", itoa(n), "stamp", stamp,
		"hb_from", orNone(hb), "hb_to", orNone(nh),
		"cp_from", orNone(cp), "cp_to", orNone(nc),
	)
	switch {
	case nh != model || nc != model:
		res.Token = "hb_mismatch"
	case !json.Valid([]byte(t)):
		res.Token = "hb_invalid_json"
	}
	return res, nil
}
