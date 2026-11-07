package fb

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/ronda2009/forgeboard/internal/budget"
)

func waitRouterTier(ctx context.Context, base, tier string) bool {
	Step("step_tier_wait", "tier", tier)
	for i := 0; i < 90; i++ {
		if rs := AskRouter(ctx, base); rs != nil && rs.Current == tier {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(2 * time.Second):
		}
	}
	return false
}

func Start(ctx context.Context, p Paths, m Model, dry bool) error {
	if m.Engine == "strata" {
		if rs := AskRouter(ctx, p.RouterURL); rs != nil && tierUp(rs, tierNameOf(m)) {
			fmt.Printf("%s already running (port is answering)\n", tierNameOf(m))
			return nil
		}
	} else if m.Running {
		fmt.Printf("%s already running\n", m.Key)
		return nil
	}
	if m.Engine != "llama" && m.Engine != "llama-swap" {
		if err := checkVRAM(p, m); err != nil {
			if !dry {
				return err
			}
			fmt.Println("warning (DryRun only, nothing started): " + err.Error())
		}
	}
	switch m.Engine {
	case "strata":
		if dry {
			fmt.Printf("[DryRun] GET %s/__switch?tier=%s\n", p.RouterURL, tierNameOf(m))
		if _, err := EnsureRouter(ctx, p); err != nil {
			return err
		}
			return nil
		}
		r, err := getJSON(ctx, p.RouterURL+"/__switch?tier="+tierNameOf(m))
		if err != nil {
			return fmt.Errorf("router %s did not answer: %w", p.RouterURL, err)
		}
		if r.code != 200 {
			return fmt.Errorf("__switch returned HTTP %d: %s", r.code, strings.TrimSpace(string(r.body)))
		}
		if waitRouterTier(ctx, p.RouterURL, tierNameOf(m)) {
			RecordRun(ctx, m.Key)
		}
		return nil
	case "llama", "llama-swap":
		s, err := NewSwap(p)
		if err != nil {
			return err
		}
		pl, args := PlanStart(p, ModelGGUF(p, m))
		if dry {
			fmt.Println(drySwapLine(p, m, pl, args))
			if pl.Reason != "" {
				fmt.Println("[DryRun] verdict: " + pl.Reason)
			}
			return nil
		}
		if pl.Code != "" && pl.Code != "no_meta" {
			return &PlanError{Code: pl.Code, Args: pl.Args, Raw: pl.Reason} // 判不过：不动任何东西，直接说清差多少
		}
		if args != "" {
			changed, err := syncSwapCmd(p, m.Key, args)
			if err != nil {
				return err
			}
			if changed {
				if busy := otherRunning(ctx, s, m.Key); len(busy) > 0 {
					return &PlanError{Code: "restart_needed", Args: map[string]string{
						"model": m.Key, "running": strings.Join(busy, ", "),
					}, Raw: fmt.Sprintf("changing %s needs the swapper restarted to take effect, which would also unload %s - stop those first", m.Key, strings.Join(busy, ", "))}
				}
				LogAction("config " + m.Key + " -> " + args + " (computed; basis: " + pl.Why + ")")
			}
		}
		if err := s.EnsureUp(ctx, p); err != nil {
			return err
		}
		LogAction("start " + m.Key + ": " + orDash(pl.Why))
		fmt.Println("computed: " + orDash(pl.Why))
		if err := s.Start(ctx, m.Key, m.Embed); err != nil {
			return explainStartFail(p, m, err)
		}
		RecordRun(ctx, m.Key) // 请求能回来说明模型已加载完 → 此刻量的是准数
		return nil
	default:
		return fmt.Errorf("engine %q not supported yet (run: forgectl doctor)", m.Engine)
	}
}

func Stop(ctx context.Context, p Paths, m Model, dry bool) error {
	switch m.Engine {
	case "strata":
		if m.Port == 0 {
			if rs := AskRouter(ctx, p.RouterURL); rs != nil {
				m.Port = rs.Tiers[tierNameOf(m)]
			}
		}
		if m.Port == 0 {
			return fmt.Errorf("tier %s has no port info (run: forgectl scan)", m.Key)
		}
		if dry {
			fmt.Printf("[DryRun] taskkill the process tree listening on port %d\n", m.Port)
			return nil
		}
		if err := killOnPort(m.Port); err != nil {
			return err
		}
		waitPortDown(m.Port)
		return nil
	case "llama", "llama-swap":
		s, err := NewSwap(p)
		if err != nil {
			return err
		}
		if dry {
			fmt.Printf("[DryRun] POST /api/models/unload/%s\n", m.Key)
			return nil
		}
		if err := s.Health(ctx); err != nil {
			fmt.Println("llama-swap is not running - nothing to unload")
			return nil
		}
		return s.StopOne(ctx, m.Key)
	default:
		return fmt.Errorf("engine %q not supported yet", m.Engine)
	}
}

func drySwapLine(p Paths, m Model, pl budget.Plan, args string) string {
	cmd := SwapCmdOf(p, m.Key)
	if cmd == "" {
		return "llama-swap loads " + m.Key + " (one chat request triggers it)"
	}
	if args != "" {
		cmd = setCmdFlags(cmd, args) // 预演里就显示算出来的值，别让人看到一份不会执行的旧命令
	}
	return "[DryRun] " + cmd + "\n[DryRun] computed: " + orDash(pl.Why)
}

func otherRunning(ctx context.Context, s *Swap, skip string) []string {
	var out []string
	for _, id := range s.Running(ctx) {
		if id != skip {
			out = append(out, id)
		}
	}
	return out
}

func orDash(s string) string {
	if s == "" {
		return "(keeping the parameters from the config file)"
	}
	return s
}

func tierUp(rs *RouterStatus, tier string) bool {
	if rs == nil {
		return false
	}
	port, ok := rs.Tiers[tier]
	return ok && port > 0 && PortUp(port)
}

func waitPortDown(port int) {
	for i := 0; i < 50; i++ {
		if !PortUp(port) {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func tierNameOf(m Model) string {
	if m.Tier != "" {
		return m.Tier
	}
	if m.GGUF != "" {
		return strings.TrimSuffix(m.GGUF, ".gguf")
	}
	return strings.TrimPrefix(m.Name, "tier ")
}

//
func checkVRAM(p Paths, m Model) error {
	if m.Need == 0 {
		return nil // 引擎报的条目没有体积信息，不拦
	}
	gpus, err := GPUs()
	if err != nil {
		return nil // nvidia-smi 不可用（如纯 AMD/无卡）不拦，doctor 会说
	}
	best, sum := 0, 0
	for _, g := range gpus {
		sum += g.Free
		if g.Free > best {
			best = g.Free
		}
	}
	limit := best
	mode := "biggest single card"
	if m.Engine == "strata" {
		limit = sum
		mode = "all cards combined"
	}
	if m.Need > limit {
		return fmt.Errorf("not enough VRAM: needs ~%d MiB, %s free only %d MiB - stop one first", m.Need, mode, limit)
	}
	return nil
}

func killOnPort(port int) error {
	out, err := exec.Command("netstat", "-ano").Output()
	if err != nil {
		return err
	}
	suffix := fmt.Sprintf(":%d", port)
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Fields(line)
		if len(f) < 5 || f[0] != "TCP" {
			continue
		}
		if strings.HasSuffix(f[1], suffix) && strings.EqualFold(f[3], "LISTENING") {
			pid := f[4]
			if runtime.GOOS == "windows" {
				return exec.Command("taskkill", "/PID", pid, "/T", "/F").Run()
			}
			return exec.Command("kill", "-9", pid).Run()
		}
	}
	return fmt.Errorf("no listener on port %d (already stopped?)", port)
}

func SwapCmdOf(p Paths, id string) string {
	b, err := os.ReadFile(filepath.Join(CfgDir(), "swap-config.json"))
	if err != nil {
		return ""
	}
	var v struct {
		Models map[string]struct {
			Cmd string `json:"cmd"`
		} `json:"models"`
	}
	if json.Unmarshal(b, &v) != nil {
		return ""
	}
	return v.Models[id].Cmd
}

func findMMProj(dir, gguf string) string {
	base := strings.TrimSuffix(gguf, ".gguf")
	ents, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	tries := []string{base}
	if i := strings.LastIndex(base, "."); i > 0 {
		tries = append(tries, base[:i])
	}
	for _, pre := range tries {
		for _, e := range ents {
			if e.IsDir() {
				continue
			}
			n := e.Name()
			if strings.HasPrefix(n, pre) && strings.Contains(strings.ToLower(n), "mmproj") {
				return filepath.Join(dir, n)
			}
		}
	}
	return ""
}

//
//
//
func WriteSwapConfig(p Paths, force bool) (string, int, error) {
	cfg := filepath.Join(CfgDir(), "swap-config.json")
	if _, err := os.Stat(cfg); err == nil && !force {
		return cfg, 0, nil
	}
	modelsDir, llamaExe := p.MODELS, p.LLAMA_EXE
	if llamaExe == "" {
		return "", 0, fmt.Errorf("LLAMA_EXE not set in forgeboard.json - cannot build the start command")
	}
	rows := ScanGGUF(modelsDir)
	if len(rows) == 0 {
		return "", 0, fmt.Errorf("no .gguf files found under %s", modelsDir)
	}
	cards := Cards(llamaExe) // 卡的情况只探一次，所有模型共用同一份账
	type cmdEntry struct{ ID, Cmd string }
	var ents []cmdEntry
	seen := map[string]bool{}
	for _, m := range rows {
		id := m.Key
		for seen[id] {
			id += "2"
		}
		seen[id] = true
		file := filepath.Join(modelsDir, m.GGUF)
		cmd := llamaExe + " -m " + file + " -ngl 99"
		if meta, ok := GGUFMeta(file); ok {
			pl := budget.Solve(meta, cards)
			if pl.Code != "" {
				cmd += fmt.Sprintf(" -c %d", budget.NominalCtx(meta, pl))
			} else {
				cmd += fmt.Sprintf(" -c %d -ctk %s -ctv %s", pl.Ctx, pl.KV, pl.KV)
				if pl.Card.Label != "" {
					cmd += " --device " + pl.Card.Label
				}
			}
		} else {
			ctxv := m.Ctx
			if ctxv == 0 {
				ctxv = 32768
			}
			cmd += fmt.Sprintf(" -c %d", ctxv)
		}
		cmd += " --host 127.0.0.1 --port ${PORT} --jinja"
		if m.Embed {
			cmd += " --embeddings" // 没这个 llama-server 对 /v1/embeddings 回 501
		}
		if m.VL {
			if mm := findMMProj(modelsDir, m.GGUF); mm != "" {
				cmd += " --mm " + mm + " --image-min-tokens 1024" // 视觉模型没投影层就是半残
			}
		}
		ents = append(ents, cmdEntry{id, cmd})
	}
	ids := make([]string, 0, len(ents))
	for _, e := range ents {
		ids = append(ids, e.ID)
	}
	var b strings.Builder
	b.WriteString(`{
  "$schema": "https://raw.githubusercontent.com/mostlygeek/llama-swap/main/config-schema.json",`)
	b.WriteString("\n" + groupsJSON(cards, ids))
	b.WriteString(`  "models": {
`)
	for i, e := range ents {
		comma := ","
		if i == len(ents)-1 {
			comma = ""
		}
		fmt.Fprintf(&b, "    %q: { \"cmd\": %q, \"name\": %q, \"ttl\": 1800 }%s\n", e.ID, e.Cmd, e.ID, comma)
	}
	b.WriteString("  }\n}\n")
	if err := os.WriteFile(cfg, []byte(b.String()), 0o644); err != nil {
		return "", 0, err
	}
	return cfg, len(ents), nil
}

//
//
//
func groupsJSON(cards []budget.Card, ids []string) string {
	vis := 0
	for _, c := range cards {
		if c.Visible {
			vis++
		}
	}
	if vis < 2 || len(ids) == 0 {
		return "\n"
	}
	q := make([]string, 0, len(ids))
	for _, id := range ids {
		q = append(q, "      "+fmt.Sprintf("%q", id))
	}
	return "  \"groups\": {\n" +
		"    \"forgeboard-coexist\": {\n" +
		"      \"swap\": false,\n" +
		"      \"exclusive\": false,\n" +
		"      \"members\": [\n" + strings.Join(q, ",\n") + "\n      ]\n" +
		"    }\n  },\n"
}
