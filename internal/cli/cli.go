package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/ronda2009/forgeboard/internal/fb"
)

const Version = "2.5.0"

var Commands = []string{"scan", "list", "start", "stop", "status", "calib", "doctor", "plan", "reload", "usage", "version"}

func IsCommand(word string) bool {
	for _, c := range Commands {
		if c == word {
			return true
		}
	}
	return word == "-version" || word == "--version"
}

func Run(ctx context.Context, args []string) error {
	cmd, rest := args[0], args[1:]
	switch cmd {
	case "-version", "--version", "version":
		fmt.Println("forgectl " + Version)
		return nil
	case "scan", "list":
		return CmdScan(ctx, rest)
	case "start":
		return CmdChange(ctx, true, rest)
	case "stop":
		return CmdChange(ctx, false, rest)
	case "status":
		return CmdStatus(ctx)
	case "calib":
		return CmdCalib(ctx)
	case "doctor":
		return CmdDoctor(ctx)
	case "plan":
		return CmdPlan(ctx, rest)
	case "reload":
		return CmdReload(ctx, rest)
	case "usage":
		return CmdUsage(ctx, rest)
	}
	return fmt.Errorf("unknown command %q (run: forgectl)", cmd)
}

func Usage() {
	fmt.Println("forgectl " + Version + " - ForgeBoard: local model service console")
	fmt.Println("usage:")
	fmt.Println("  forgectl scan            discover models: gguf dirs + engines + running services (3-way merge)")
	fmt.Println("  forgectl start <key>     start an entry (--dry = print what would run)")
	fmt.Println("  forgectl stop [key]      stop / unload (no key = unload what llama-swap has loaded)")
	fmt.Println("  forgectl status          what is running now (router current + swap running)")
	fmt.Println("  forgectl doctor          what this machine has / lacks, in plain words")
	fmt.Println("  forgectl calib           measure real VRAM use now and record it (read-only)")
	fmt.Println("  forgectl plan <key>      what this machine would give it: ctx / KV / which card (read-only)")
	fmt.Println("  forgectl reload          restart the swapper so a changed swap-config.json takes effect (--dry)")
	fmt.Println("  forgectl -version        print version")
}

//
func ScanMerge(ctx context.Context, p fb.Paths) []fb.Model {
	byKey := map[string]fb.Model{}

	if p.MODELS != "" {
		for _, m := range fb.ScanGGUF(p.MODELS) {
			byKey[m.Key] = m
		}
	}

	if rs := fb.AskRouter(ctx, p.RouterURL); rs != nil {
		up := fb.PortsUp(ctx, portsOf(rs.Tiers))
		for tier, port := range rs.Tiers {
			k := fb.ModelKey(tier + ".gguf")
			m, has := byKey[k]
			if !has {
				m = fb.Model{Key: k, Name: "tier " + tier, Tier: tier, Engine: "strata", Source: "engine:strata"}
			}
			m.Port = port
			m.Running = up[port]
			byKey[k] = m
		}
	} else {
		for tier, port := range fb.RouterTiers(p) {
			k := fb.ModelKey(tier + ".gguf")
			if _, has := byKey[k]; has {
				continue // 目录里真有同名 gguf 就以目录为准（已有条目优先）
			}
			byKey[k] = fb.Model{Key: k, Name: "tier " + tier, Tier: tier, Engine: "strata", Port: port, Source: "tier:" + tier}
		}
	}

	if s, err := fb.NewSwap(p); err == nil && s.Health(ctx) == nil {
		ids := map[string]bool{}
		ms, _ := s.Models(ctx)
		for _, m := range ms {
			ids[m.Key] = true
			if _, has := byKey[m.Key]; !has {
				m.Engine = "llama-swap"
				m.Source = "engine:llama-swap"
				byKey[m.Key] = m
			}
		}
		run := map[string]bool{}
		for _, id := range s.Running(ctx) {
			run[id] = true
		}
		for k, m := range byKey {
			if ids[k] {
				m.Engine = "llama-swap" // 这些 gguf 由 llama-swap 代管，起停走它
				m.Running = m.Running || run[k]
				byKey[k] = m
			}
		}
	}

	seenPort := map[int]bool{}
	var ports []int
	addPort := func(n int) {
		if n > 0 && !seenPort[n] {
			seenPort[n] = true
			ports = append(ports, n)
		}
	}
	for _, n := range p.CandidatePorts {
		addPort(n)
	}
	if oc, err := fb.LoadOCConfig(p); err == nil {
		for _, pr := range oc.Providers {
			if pr.Host() == "local" {
				addPort(pr.Port())
			}
		}
	}
	sort.Ints(ports)
	routerPorts := map[int]bool{}
	if rs := fb.AskRouter(ctx, p.RouterURL); rs != nil {
		for _, port := range rs.Tiers {
			routerPorts[port] = true
		}
	}
	for _, svc := range fb.ProbePorts(ctx, ports) {
		if svc.Router || routerPorts[svc.Port] || svc.Port == p.SwapPort {
			continue // 路由器与 llama-swap 管的端口不另记一条（否则同一服务出现两遍）
		}
		k := fb.ModelKey(svc.Model)
		m, has := byKey[k]
		if !has {
			m = fb.Model{Key: k, Name: svc.Model, Engine: svc.Kind, Source: fmt.Sprintf("port:%d", svc.Port)}
		}
		m.Running = true
		m.Port = svc.Port
		byKey[k] = m
	}

	var rows []fb.Model
	for _, m := range byKey {
		rows = append(rows, m)
	}
	fb.ApplyCalib(rows) // 有实测用实测（+512 余量），没实测用体积估算 - 不写死任何表
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Need != rows[j].Need {
			return rows[i].Need < rows[j].Need
		}
		return rows[i].Key < rows[j].Key // 同 need 时按 key 定序，免得界面每次刷新跳来跳去
	})
	return rows
}

func CmdScan(ctx context.Context, args []string) error {
	p := fb.LoadPaths()
	rows := ScanMerge(ctx, p)
	if cal := fb.AutoCalibrate(ctx, p, rows); cal != nil { // 自动评估（只读）：量到就当场用上
		fmt.Println("calib:", fb.CalibLine(cal))
		fb.ApplyCalib(rows)
	}
	if contains(args, "--json") {
		b, _ := json.MarshalIndent(rows, "", "  ")
		fmt.Println(string(b))
		return nil
	}
	w := tabwriter.NewWriter(os.Stdout, 2, 4, 2, ' ', 0)
	fmt.Fprintln(w, "KEY\tNAME\tNEED(MiB)\tENGINE\tPORT\tRUNNING\tSOURCE")
	for _, m := range rows {
		run := ""
		if m.Running {
			run = "*"
		}
		need := fmt.Sprintf("%d", m.Need)
		if m.Measured {
			need += " (measured)" // 实测校准过的标出来，让人知道这个数不是估的
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%d\t%s\t%s\n", m.Key, trunc(m.Name, 44), need, m.Engine, m.Port, run, m.Source)
	}
	w.Flush()
	fmt.Printf("%d entries (scan root: %s)\n", len(rows), p.MODELS)
	fmt.Println("calib:", fb.CalibStatsLine())
	return nil
}

func CmdCalib(ctx context.Context) error {
	p := fb.LoadPaths()
	rows := ScanMerge(ctx, p)
	var running []string
	for _, m := range rows {
		if m.Running && m.Engine != "other" {
			running = append(running, m.Key)
		}
	}
	if len(running) > 1 {
		return fmt.Errorf("%d models running at once - cannot attribute VRAM to one of them", len(running))
	}
	key := ""
	if len(running) == 1 {
		key = running[0]
	}
	cal, err := fb.Calibrate(ctx, p, key)
	if err != nil {
		return err
	}
	if cal == nil {
		return fmt.Errorf("engine is up but no model could be identified - nothing recorded")
	}
	fmt.Println(fb.CalibLine(cal))
	return nil
}

func CmdDoctor(ctx context.Context) error {
	p := fb.LoadPaths()
	fmt.Println("config dir:", fb.CfgDir())
	reportLine("MODELS dir", p.MODELS, existsDir)
	reportLine("llama-server", p.LLAMA_EXE, existsFile)
	reportLine("tier dir", p.TIER_DIR, existsDir)
	if gpus, err := fb.GPUs(); err == nil {
		for _, g := range gpus {
			fmt.Printf("GPU %d: %s  %d/%d MiB free\n", g.Index, g.Name, g.Free, g.Total)
		}
	} else {
		fmt.Println("GPU: nvidia-smi unavailable:", err)
	}
	if rs := fb.AskRouter(ctx, p.RouterURL); rs != nil {
		fmt.Printf("router %s: current=%s tiers=%d\n", p.RouterURL, rs.Current, len(rs.Tiers))
	} else {
		fmt.Println("router " + p.RouterURL + ": no answer (not running, or wrong address in forgeboard.json)")
	}
	if s, err := fb.NewSwap(p); err != nil {
		fmt.Println("llama-swap:", err)
	} else if err := s.Health(ctx); err != nil {
		fmt.Println("llama-swap: not running (start brings it up)")
	} else {
		fmt.Println("llama-swap: running, control port", p.SwapPort)
	}
	if p.MODELS != "" && p.LLAMA_EXE != "" {
		cfg, n, err := fb.WriteSwapConfig(p, false)
		if err != nil {
			fmt.Println("swap config:", err)
		} else if n > 0 {
			fmt.Printf("swap config: generated %s (%d models)\n", cfg, n)
		} else {
			fmt.Println("swap config: already exists", cfg)
		}
	}
	fmt.Println("calib:", fb.CalibStatsLine())
	fmt.Println("candidate ports:", p.CandidatePorts, "(set CandidatePorts in forgeboard.json)")
	return nil
}

func CmdChange(ctx context.Context, start bool, args []string) error {
	dry := contains(args, "--dry")
	var key string
	for _, a := range args {
		if a != "--dry" {
			key = a
			break
		}
	}
	p := fb.LoadPaths()

	if key == "" && !start {
		res, err := fb.StopAll(ctx, p, dry, false)
		if err != nil {
			return err
		}
		fmt.Printf("stop-all dry=%v ports=%s killed=%s keep=%s unload=%s still=%s\n",
			dry, res.Args["ports"], res.Args["killed"], res.Args["keep"], res.Args["unload"], res.Args["still"])
		if res.Note != "" {
			fmt.Println("note:", res.Note)
		}
		return nil
	}
	if key == "" {
		return fmt.Errorf("start needs a model key (run: forgectl scan)")
	}
	rows := ScanMerge(ctx, p)
	var hit *fb.Model
	for i := range rows {
		if rows[i].Key == key {
			hit = &rows[i]
			break
		}
	}
	if hit == nil {
		return fmt.Errorf("no entry %q (run: forgectl scan)", key)
	}
	if start {
		return fb.Start(ctx, p, *hit, dry)
	}
	return fb.Stop(ctx, p, *hit, dry)
}

func CmdPlan(ctx context.Context, args []string) error {
	p := fb.LoadPaths()
	var key string
	for _, a := range args {
		if !strings.HasPrefix(a, "--") {
			key = a
			break
		}
	}
	rows := ScanMerge(ctx, p)
	if key == "" {
		for _, m := range rows {
			if m.Engine == "llama" || m.Engine == "llama-swap" {
				if err := planOne(p, m); err != nil {
					fmt.Println(m.Key+":", err)
				}
			}
		}
		return nil
	}
	for _, m := range rows {
		if m.Key == key {
			return planOne(p, m)
		}
	}
	return fmt.Errorf("no entry %q (run: forgectl scan)", key)
}

func CmdUsage(ctx context.Context, args []string) error {
	live := contains(args, "--live")
	s, note := fb.ReadUsage()
	if live || s == nil {
		got, err := fb.FetchUsage(ctx, fb.LoadPaths())
		if err != nil {
			return err
		}
		if err := fb.WriteUsage(got); err != nil {
			return err
		}
		s = got
	} else if note != "" {
		return fmt.Errorf("%s (run: forgectl usage --live)", note)
	}
	fmt.Printf("collected %s ago=%ds cache=%s range=%s..%s windows=%d\n",
		time.UnixMilli(s.At).Format("2006-01-02 15:04:05"), s.AgeSec, s.Cache, s.Start, s.End, s.Windows)
	fmt.Printf("today tokens=%d cost=%.4f | 7d tokens=%d cost=%.4f | 30d tokens=%d cost=%.4f\n",
		s.Today.Tokens, s.Today.Cost, s.D7.Tokens, s.D7.Cost, s.Totals.Tokens, s.Totals.Cost)
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "MODEL\tTODAY\t7D\t30D\tCOST")
	for _, m := range s.Models {
		cost := "-"
		if m.Paid {
			cost = fmt.Sprintf("%.4f", m.Cost)
		}
		fmt.Fprintf(w, "%s/%s\t%d\t%d\t%d\t%s\n", m.Provider, m.Model, m.Today, m.D7, m.D30, cost)
	}
	return w.Flush()
}
func planOne(p fb.Paths, m fb.Model) error {
	pl, args := fb.PlanStart(p, fb.ModelGGUF(p, m))
	fmt.Printf("%s\n", m.Key)
	if pl.Ctx > 0 {
		fmt.Printf("  ctx      %d\n", pl.Ctx)
		fmt.Printf("  kv       %s (%d KB/token)\n", pl.KV, pl.KVPerTokKB)
		fmt.Printf("  needs    ~%d MiB\n", pl.NeedMiB)
		fmt.Printf("  card     %s [%s]  free %d MiB\n", pl.Card.Name, pl.Card.Label, pl.Card.FreeMiB)
		fmt.Printf("  flags    %s\n", args)
	}
	if pl.Why != "" {
		fmt.Printf("  why      %s\n", pl.Why)
	}
	if pl.Code != "" {
		fmt.Printf("  verdict  %s\n", pl.Code)
		fmt.Printf("           %s\n", pl.Reason)
	}
	return nil
}

func CmdReload(ctx context.Context, args []string) error {
	dry := contains(args, "--dry")
	r, err := fb.ReloadSwapOp(ctx, fb.LoadPaths(), dry)
	if err != nil {
		return err
	}
	fmt.Println("token:", r.Token)
	for _, k := range sortedArgKeys(r.Args) {
		fmt.Printf("  %-8s %s\n", k, r.Args[k])
	}
	if r.Note != "" {
		fmt.Println("  note:", r.Note)
	}
	return nil
}

func sortedArgKeys(m map[string]string) []string {
	var ks []string
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

func CmdStatus(ctx context.Context) error {
	p := fb.LoadPaths()
	if rs := fb.AskRouter(ctx, p.RouterURL); rs != nil {
		fmt.Printf("router: current=%s  tiers=%d\n", rs.Current, len(rs.Tiers))
	} else {
		fmt.Println("router: no answer")
	}
	if s, err := fb.NewSwap(p); err == nil && s.Health(ctx) == nil {
		run := s.Running(ctx)
		if len(run) == 0 {
			fmt.Println("llama-swap: running, no model loaded")
		} else {
			fmt.Println("llama-swap running, loaded:", strings.Join(run, ", "))
		}
		if hash, stale, err := fb.SwapConfigFingerprint(); err == nil {
			if len(hash) > 12 {
				hash = hash[:12]
			}
			if stale {
				fmt.Printf("llama-swap config: CHANGED since the engine started (hash %s) - run: forgectl reload\n", hash)
			} else {
				fmt.Printf("llama-swap config: engine runs this one (hash %s)\n", hash)
			}
		}
	}
	return nil
}

func reportLine(label, path string, ok func(string) bool) {
	if path == "" {
		fmt.Printf("%s: not configured (set it in forgeboard.json)\n", label)
		return
	}
	state := "MISSING"
	if ok(path) {
		state = "ok"
	}
	fmt.Printf("%s: %s  %s\n", label, state, path)
}

func existsDir(p string) bool  { st, e := os.Stat(p); return e == nil && st.IsDir() }
func existsFile(p string) bool { st, e := os.Stat(p); return e == nil && !st.IsDir() }

func contains(a []string, s string) bool {
	for _, x := range a {
		if x == s {
			return true
		}
	}
	return false
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func portsOf(tiers map[string]int) []int {
	seen := map[int]bool{}
	var out []int
	for _, port := range tiers {
		if port > 0 && !seen[port] {
			seen[port] = true
			out = append(out, port)
		}
	}
	sort.Ints(out)
	return out
}
