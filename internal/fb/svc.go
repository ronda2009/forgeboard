package fb

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"

	gonet "github.com/shirou/gopsutil/v3/net"
	goproc "github.com/shirou/gopsutil/v3/process"
)

type ServiceControl struct {
	Start bool `json:"start"`
	Stop  bool `json:"stop"`
	Rest  bool `json:"rest"`  // 重启：走它自己的优雅起停
	Force bool `json:"force"` // 强制重启：直接杀进程树再起
}

type ServiceLocation struct {
	How  string `json:"how,omitempty"`
	Path string `json:"path,omitempty"`
}

func Services(ctx context.Context, p Paths, oc *OCConfig) []Service {
	var out []Service

	gport := 18789
	if oc != nil && oc.GatewayPort > 0 {
		gport = oc.GatewayPort
	}
	out = append(out, Service{Name: "OpenClaw gateway", Kind: "gateway", Port: gport, Up: PortUp(gport),
		Where: gatewayLocation(p, gport)})

	rport := portOfURL(p.RouterURL)
	if rport > 0 {
		out = append(out, Service{Name: "Model router", Kind: "router", Port: rport, URL: p.RouterURL,
			Up: PortUp(rport), Where: routerLocation(p, rport)})
	}

	sport := p.SwapPort
	if sport == 0 {
		sport = 8089
	}
	swInstalled := CfgHasSwap()
	out = append(out, Service{Name: "llama-swap (adapter)", Kind: "adapter", Port: sport, Up: PortUp(sport),
		Installed: &swInstalled, Where: swapLocation(sport)})

	ollamaInstalled := ollamaInstalled()
	ollamaUp := PortUp(11434)
	out = append(out, Service{Name: "Ollama", Kind: "engine", Port: 11434, URL: "http://127.0.0.1:11434",
		Up: ollamaUp, Installed: &ollamaInstalled, Where: ollamaLocation(),
		Note: noteNotInstalled(ollamaInstalled, ollamaUp)})

	seen := map[int]bool{}
	if oc != nil {
		rows := make([]OCProvider, len(oc.Providers))
		copy(rows, oc.Providers)
		sort.Slice(rows, func(i, j int) bool { return rows[i].Name < rows[j].Name })
		for _, pr := range rows {
			if pr.Host() != "local" {
				continue
			}
			_, port := splitURL(pr.BaseURL)
			if port == 0 || seen[port] {
				continue
			}
			seen[port] = true
			out = append(out, Service{Name: "provider:" + pr.Name, Kind: "engine", Port: port, URL: pr.BaseURL,
				Up: PortUp(port), Where: engineLocation(p, port),
				Note: "note-mounted:" + itoa(len(pr.Models))})
		}
	}

	if oc != nil {
		for _, pr := range oc.Providers {
			if pr.Host() == "local" || pr.BaseURL == "" {
				continue
			}
			out = append(out, Service{Name: "provider:" + pr.Name, Kind: "api", URL: pr.BaseURL, Up: false,
				Where: ServiceLocation{How: "where-remote"}, Note: "note-remote"})
		}
	}
	for i := range out {
		out[i].Ctl = controlOf(out[i], p)
	}
	return out
}

func controlOf(s Service, p Paths) ServiceControl {
	if s.Kind == "api" {
		return ServiceControl{}
	}
	none := ServiceControl{}
	if s.Installed != nil && !*s.Installed {
		return none
	}
	switch s.Kind {
	case "gateway":
		if !NewOC(p).Available() {
			return none
		}
	case "router":
		if routerScriptDir(p) == "" {
			return none
		}
	case "adapter":
		if !fileExists(filepath.Join(CfgDir(), "llama-swap.exe")) {
			return none
		}
	case "engine":
		if !startableEngine(p, s.Port) {
			return none
		}
	default:
		return none
	}
	return ServiceControl{Start: true, Stop: true, Rest: true, Force: true}
}

func startableEngine(p Paths, port int) bool {
	if port == p.SwapPort || port == portOfURL(p.RouterURL) {
		return true
	}
	return fileExists(engineExe(p, port)) || fileExists(p.LLAMA_EXE)
}

func ControlService(ctx context.Context, p Paths, kind string, port int, act string, dry bool) (*OpResult, error) {
	exe := svcExePath(p, kind, port)
	switch act {
	case "stop":
		if dry {
			return op("svc_stop_dry", true).subj("port", itoa(port)), nil
		}
		if err := killOnPort(port); err != nil {
			return nil, err
		}
		return op("svc_stop_done", false).subj("port", itoa(port)), nil
	case "start":
		if dry {
			return op("svc_start_dry", true).subj("kind", kind, "port", itoa(port), "path", orNone(exe)), nil
		}
		got, err := ensureService(ctx, p, kind, port)
		if err != nil {
			return nil, err
		}
		return op("svc_start_done", false).subj("kind", kind, "port", itoa(port), "path", orNone(got)), nil
	case "rest", "force":
		cmd, cwd := observedCmd(port)
		if kind == "gateway" && act == "rest" {
			oc := NewOC(p)
			if dry {
				return op("svc_gw_rest_dry", true).subj("path", orNone(oc.CLI)), nil
			}
			Step("step_gw_restart", "path", orNone(oc.CLI))
			Step("step_gw_wait", "port", itoa(port))
			if _, err := oc.RestartGateway(ctx, port); err != nil {
				Step("step_gw_fail", "path", orNone(oc.CLI), "port", itoa(port))
				return nil, err
			}
			return op("svc_gw_rest_done", false).subj("path", orNone(oc.CLI)), nil
		}
		if dry {
			return op("svc_rest_dry", true).subj("port", itoa(port), "path", orNone(cmd)), nil
		}
		if !PortUp(port) {
			got, err := ensureService(ctx, p, kind, port)
			if err != nil {
				return nil, err
			}
			return op("svc_start_done", false).subj("kind", kind, "port", itoa(port), "path", orNone(got)), nil
		}
		Step("step_svc_kill", "port", itoa(port), "cmd", orNone(cmd))
		if err := killOnPort(port); err != nil {
			return nil, err
		}
		if cmd == "" {
			got, err := ensureService(ctx, p, kind, port)
			if err != nil {
				return nil, err
			}
			return op("svc_rest_done", false).subj("port", itoa(port), "path", orNone(got)), nil
		}
		Step("step_svc_start", "port", itoa(port), "cmd", filepath.Base(cmd))
		if err := runCmdLine(ctx, cmd, cwd, port); err != nil {
			return nil, err
		}
		return op("svc_rest_done", false).subj("port", itoa(port), "path", filepath.Base(cmd)), nil
	}
	return nil, fmt.Errorf("unknown action %q", act)
}

func ensureService(ctx context.Context, p Paths, kind string, port int) (string, error) {
	switch kind {
	case "router":
		return EnsureRouter(ctx, p)
	case "adapter":
		s, err := NewSwap(p)
		if err != nil {
			return "", err
		}
		if err := s.EnsureUp(ctx, p); err != nil {
			return "", err
		}
		return filepath.Join(s.dir, "llama-swap.exe"), nil
	case "gateway":
		oc := NewOC(p)
		if !oc.Available() {
			return "", fmt.Errorf("openclaw command line not found (set OC_CLI in forgeboard.json)")
		}
		if _, err := oc.StartGateway(ctx, port); err != nil {
			return "", err
		}
		return oc.CLI, nil
	case "engine":
		if port == p.SwapPort {
			s, err := NewSwap(p)
			if err != nil {
				return "", err
			}
			if err := s.EnsureUp(ctx, p); err != nil {
				return "", err
			}
			return filepath.Join(s.dir, "llama-swap.exe"), nil
		}
		if port == portOfURL(p.RouterURL) {
			return EnsureRouter(ctx, p)
		}
		if cmd, cwd := observedCmd(port); cmd != "" {
			return cmd, runCmdLine(ctx, cmd, cwd, port)
		}
		exe := engineExe(p, port)
		if exe == "" {
			exe = p.LLAMA_EXE
		}
		if !fileExists(exe) {
			return "", fmt.Errorf("cannot find the engine for port %d (fill TIER_DIR / LLAMA_EXE in forgeboard.json)", port)
		}
		args := engineArgs(p, port)
		cmd := exec.Command(exe, args...)
		cmd.Dir = engineCwd(p, port)
		cmd.SysProcAttr = detachCmd
		if err := cmd.Start(); err != nil {
			return exe, err
		}
		_ = cmd.Process.Release()
		if !waitPortUp(port, 120) {
			return exe, fmt.Errorf("port %d still not listening after 120s", port)
		}
		return exe, nil
	}
	return "", fmt.Errorf("no start path for kind %q", kind)
}

func EnsureRouter(ctx context.Context, p Paths) (string, error) {
	if AskRouter(ctx, p.RouterURL) != nil {
		return "", nil
	}
	dir := routerScriptDir(p)
	if dir == "" {
		return "", fmt.Errorf("no strata-router.py under %s (fill TIER_DIR in forgeboard.json)", tierRoot(p))
	}
	if vbs := firstWithExt(dir, "strata-start-all-hidden.vbs"); vbs != "" {
		if err := runHidden(ctx, "wscript.exe", vbs); err != nil {
			return vbs, err
		}
	} else {
		py := routerPython(dir)
		if py == "" {
			return "", fmt.Errorf("no python for %s (set PY in forgeboard.json)", filepath.Join(dir, "strata-router.py"))
		}
		cmd := exec.Command(py, filepath.Join(dir, "strata-router.py"))
		cmd.Dir = dir
		cmd.SysProcAttr = detachCmd
		if err := cmd.Start(); err != nil {
			return py, err
		}
		_ = cmd.Process.Release()
	}
	if !waitRouter(ctx, p.RouterURL, 90) {
		return dir, fmt.Errorf("router still not answering on %s after 90s (see %s\\strata-router*.log)", p.RouterURL, dir)
	}
	return dir, nil
}

func EnsureTierEngine(ctx context.Context, p Paths, tier string, port int) error {
	root := tierRoot(p)
	if bat := firstWithExt(root, "run-"+tier+".bat"); bat != "" {
		return runHidden(ctx, bat, "")
	}
	srv := filepath.Join(root, "serve", "server.py")
	if !fileExists(srv) {
		return fmt.Errorf("cannot find %s (fill TIER_DIR in forgeboard.json)", srv)
	}
	cfg := filepath.Join(root, "strata-"+tier+".json")
	if !fileExists(cfg) {
		return fmt.Errorf("cannot find %s", cfg)
	}
	py := routerPython(root)
	if py == "" {
		return fmt.Errorf("no python for %s (set PY in forgeboard.json)", srv)
	}
	cmd := exec.Command(py, srv, "--engine", "strata", "--config", cfg, "--port", itoa(port))
	cmd.Dir = root
	cmd.SysProcAttr = detachCmd
	if err := cmd.Start(); err != nil {
		return err
	}
	_ = cmd.Process.Release()
	if !waitPortUp(port, 120) {
		return fmt.Errorf("port %d still not listening after 120s", port)
	}
	return nil
}

func RouterTiers(p Paths) map[string]int {
	dir := routerScriptDir(p)
	if dir == "" {
		return nil
	}
	b, err := os.ReadFile(filepath.Join(dir, "strata-router.py"))
	if err != nil {
		return nil
	}
	out := map[string]int{}
	for _, m := range routerTierRe.FindAllStringSubmatch(string(b), -1) {
		out[m[1]] = atoiSafe(m[2])
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

var routerTierRe = regexp.MustCompile(`'([A-Za-z0-9_.-]+)'\s*:\s*\{[^{}]*?'port'\s*:\s*(\d+)`)

func portOfURL(u string) int {
	_, port := splitURL(u)
	return port
}

func fileExists(p string) bool {
	if p == "" {
		return false
	}
	_, err := os.Stat(p)
	return err == nil
}

func firstWithExt(dir, name string) string {
	if dir == "" {
		return ""
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	want := strings.ToLower(name)
	for _, e := range entries {
		if !e.IsDir() && strings.ToLower(e.Name()) == want {
			return filepath.Join(dir, e.Name())
		}
	}
	return ""
}

func tierRoot(p Paths) string {
	root := p.TIER_DIR
	if root == "" {
		return ""
	}
	if fileExists(filepath.Join(root, "serve", "server.py")) {
		return root
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return root
	}
	var subs []string
	for _, e := range entries {
		if e.IsDir() && fileExists(filepath.Join(root, e.Name(), "serve", "server.py")) {
			subs = append(subs, e.Name())
		}
	}
	if len(subs) > 0 {
		sort.Strings(subs)
		return filepath.Join(root, subs[0])
	}
	return root
}

func routerScriptDir(p Paths) string {
	if d := p.SERVE; fileExists(filepath.Join(d, "strata-router.py")) {
		return d
	}
	root := tierRoot(p)
	for _, dir := range []string{root, filepath.Dir(root)} {
		if fileExists(filepath.Join(dir, "strata-router.py")) {
			return dir
		}
	}
	return ""
}

func routerPython(dir string) string {
	if py := LoadPaths().PY; fileExists(py) {
		return py
	}
	if v := filepath.Join(dir, ".venv", "Scripts", "python.exe"); fileExists(v) {
		return v
	}
	if v := filepath.Join(dir, ".venv", "bin", "python"); fileExists(v) {
		return v
	}
	for _, name := range []string{"python", "python3"} {
		if exe, err := exec.LookPath(name); err == nil {
			return exe
		}
	}
	return ""
}

func listeningPID(port int) int32 {
	conns, err := gonet.Connections("tcp")
	if err != nil {
		return 0
	}
	for _, c := range conns {
		if c.Status == "LISTEN" && int(c.Laddr.Port) == port && c.Pid > 0 {
			return c.Pid
		}
	}
	return 0
}

func procExe(pid int32) string {
	if pid <= 0 {
		return ""
	}
	pr, err := goproc.NewProcess(pid)
	if err != nil {
		return ""
	}
	exe, err := pr.Exe()
	if err != nil {
		return ""
	}
	return exe
}

func observedCmd(port int) (string, string) {
	pid := listeningPID(port)
	if pid <= 0 {
		return "", ""
	}
	pr, err := goproc.NewProcess(pid)
	if err != nil {
		return "", ""
	}
	cmd, err := pr.Cmdline()
	if err != nil || strings.TrimSpace(cmd) == "" {
		return "", ""
	}
	cwd, _ := pr.Cwd()
	return strings.TrimSpace(cmd), cwd
}

func runCmdLine(ctx context.Context, cmdLine, cwd string, port int) error {
	name, args := splitCmdLine(cmdLine)
	if name == "" {
		return fmt.Errorf("cannot read how port %d was started (start it once by hand or fill the path in forgeboard.json)", port)
	}
	cmd := exec.Command(name, args...)
	if cwd != "" {
		cmd.Dir = cwd
	}
	cmd.SysProcAttr = detachCmd
	if err := cmd.Start(); err != nil {
		return err
	}
	_ = cmd.Process.Release()
	if !waitPortUp(port, 120) {
		return fmt.Errorf("port %d still not listening after 120s (started %s)", port, filepath.Base(name))
	}
	return nil
}

func splitCmdLine(s string) (string, []string) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", nil
	}
	var name string
	rest := s
	if strings.HasPrefix(s, `"`) {
		if i := strings.Index(s[1:], `"`); i > 0 {
			name, rest = s[1:i+1], s[i+2:]
		}
	} else {
		fields := strings.Fields(s)
		name, rest = fields[0], strings.Join(fields[1:], " ")
		if len(fields) > 1 && strings.Contains(rest, " ") && fileExists(fields[0]+" "+firstField(rest)) {
			name = fields[0] + " " + firstField(rest)
			rest = strings.Join(strings.Fields(rest)[1:], " ")
		}
	}
	return name, shellArgs(rest)
}

func firstField(s string) string {
	f := strings.Fields(strings.TrimSpace(s))
	if len(f) == 0 {
		return ""
	}
	return f[0]
}

func shellArgs(s string) []string {
	var out []string
	var cur strings.Builder
	inQ := false
	for _, r := range s {
		switch {
		case r == '"':
			inQ = !inQ
		case (r == ' ' || r == '\t') && !inQ:
			if cur.Len() > 0 {
				out = append(out, cur.String())
				cur.Reset()
			}
		default:
			cur.WriteRune(r)
		}
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}

func exeOnPath(name string) string {
	if exe, err := exec.LookPath(name); err == nil {
		return exe
	}
	return ""
}

func knownExePaths(names ...string) string {
	home, _ := os.UserHomeDir()
	var dirs []string
	if runtime.GOOS == "windows" {
		if local := os.Getenv("LOCALAPPDATA"); local != "" {
			dirs = append(dirs, filepath.Join(local, "Programs"))
		}
		dirs = append(dirs, `C:\Program Files`, `C:\Program Files (x86)`)
	} else {
		dirs = append(dirs, "/usr/local/bin", "/usr/bin")
	}
	if home != "" {
		dirs = append(dirs, home)
	}
	for _, d := range dirs {
		for _, n := range names {
			if f := firstWithExt(d, n); f != "" {
				return f
			}
		}
	}
	return ""
}

func gatewayLocation(p Paths, port int) ServiceLocation {
	if exe := procExe(listeningPID(port)); exe != "" {
		return ServiceLocation{How: "where-here", Path: exe}
	}
	if cli := NewOC(p).CLI; cli != "" {
		return ServiceLocation{How: "where-config", Path: cli}
	}
	if exe := exeOnPath("openclaw"); exe != "" {
		return ServiceLocation{How: "where-path", Path: exe}
	}
	return ServiceLocation{How: "where-unknown"}
}

func routerLocation(p Paths, port int) ServiceLocation {
	if exe := procExe(listeningPID(port)); exe != "" {
		return ServiceLocation{How: "where-here", Path: exe}
	}
	if dir := routerScriptDir(p); dir != "" {
		return ServiceLocation{How: "where-config", Path: filepath.Join(dir, "strata-router.py")}
	}
	return ServiceLocation{How: "where-unknown"}
}

func swapLocation(port int) ServiceLocation {
	if exe := procExe(listeningPID(port)); exe != "" {
		return ServiceLocation{How: "where-here", Path: exe}
	}
	if exe := filepath.Join(CfgDir(), "llama-swap.exe"); fileExists(exe) {
		return ServiceLocation{How: "where-config", Path: exe}
	}
	return ServiceLocation{How: "where-unknown"}
}

func ollamaLocation() ServiceLocation {
	if exe := procExe(listeningPID(11434)); exe != "" {
		return ServiceLocation{How: "where-here", Path: exe}
	}
	if exe := exeOnPath("ollama"); exe != "" {
		return ServiceLocation{How: "where-path", Path: exe}
	}
	if exe := knownExePaths("ollama.exe", "ollama"); exe != "" {
		return ServiceLocation{How: "where-dir", Path: exe}
	}
	return ServiceLocation{How: "where-unknown"}
}

func engineLocation(p Paths, port int) ServiceLocation {
	if exe := procExe(listeningPID(port)); exe != "" {
		return ServiceLocation{How: "where-here", Path: exe}
	}
	if port == p.SwapPort {
		return swapLocation(port)
	}
	if exe := engineExe(p, port); exe != "" {
		return ServiceLocation{How: "where-config", Path: exe}
	}
	if exe := p.LLAMA_EXE; fileExists(exe) {
		return ServiceLocation{How: "where-config", Path: exe}
	}
	return ServiceLocation{How: "where-unknown"}
}

func svcExePath(p Paths, kind string, port int) string {
	switch kind {
	case "router":
		if dir := routerScriptDir(p); dir != "" {
			if v := firstWithExt(dir, "strata-start-all-hidden.vbs"); v != "" {
				return v
			}
			return filepath.Join(dir, "strata-router.py")
		}
	case "adapter":
		return filepath.Join(CfgDir(), "llama-swap.exe")
	case "gateway":
		return NewOC(p).CLI
	case "engine":
		if cmd, _ := observedCmd(port); cmd != "" {
			return cmd
		}
		if exe := engineExe(p, port); exe != "" {
			return exe
		}
		return p.LLAMA_EXE
	}
	return ""
}

type tierJSON struct {
	Exe  string   `json:"exe"`
	Args []string `json:"args"`
	Cwd  string   `json:"cwd"`
	Port int      `json:"port"`
}

func TierConfigs(p Paths) map[string]string {
	root := tierRoot(p)
	out := map[string]string{}
	if root == "" {
		return out
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return out
	}
	var names []string
	for _, e := range entries {
		n := strings.ToLower(e.Name())
		if e.IsDir() || !strings.HasPrefix(n, "strata-") || !strings.HasSuffix(n, ".json") {
			continue
		}
		names = append(names, strings.TrimSuffix(strings.TrimPrefix(n, "strata-"), ".json"))
	}
	sort.Strings(names)
	for _, n := range names {
		f := filepath.Join(root, "strata-"+n+".json")
		if _, ok := readTierJSON(f); ok {
			out[n] = f
		}
	}
	return out
}

func readTierJSON(path string) (tierJSON, bool) {
	var j tierJSON
	if path == "" {
		return j, false
	}
	b, err := os.ReadFile(path)
	if err != nil || json.Unmarshal(b, &j) != nil || j.Exe == "" {
		return j, false
	}
	return j, true
}

func engineConfig(p Paths, port int) string {
	for _, f := range TierConfigs(p) {
		if j, ok := readTierJSON(f); ok && j.Port == port {
			return f
		}
	}
	return ""
}

func engineExe(p Paths, port int) string {
	j, _ := readTierJSON(engineConfig(p, port))
	return j.Exe
}

func engineArgs(p Paths, port int) []string {
	j, _ := readTierJSON(engineConfig(p, port))
	return append(append([]string{}, j.Args...), "--port", itoa(port))
}

func engineCwd(p Paths, port int) string {
	j, _ := readTierJSON(engineConfig(p, port))
	if j.Cwd != "" {
		return j.Cwd
	}
	return tierRoot(p)
}

func waitPortUp(port, seconds int) bool {
	for i := 0; i < seconds; i++ {
		if PortUp(port) {
			return true
		}
		time.Sleep(time.Second)
	}
	return PortUp(port)
}

func waitRouter(ctx context.Context, base string, seconds int) bool {
	for i := 0; i < seconds; i++ {
		if AskRouter(ctx, base) != nil {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(time.Second):
		}
	}
	return AskRouter(ctx, base) != nil
}

func runHidden(ctx context.Context, name, arg string) error {
	cctx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()
	var cmd *exec.Cmd
	if arg == "" {
		cmd = exec.CommandContext(cctx, name)
	} else {
		cmd = exec.CommandContext(cctx, name, arg)
	}
	cmd.SysProcAttr = hideCmd
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s failed: %w (%s)", filepath.Base(name), err, trunc(strings.TrimSpace(string(out)), 160))
	}
	return nil
}

func ollamaInstalled() bool {
	if exeOnPath("ollama") != "" {
		return true
	}
	home, _ := os.UserHomeDir()
	cands := []string{
		filepath.Join(home, "AppData", "Local", "Programs", "Ollama", "ollama.exe"),
		"C:/Program Files/Ollama/ollama.exe",
		"/usr/local/bin/ollama",
		"/usr/bin/ollama",
	}
	for _, c := range cands {
		if fileExists(c) {
			return true
		}
	}
	return false
}

func noteNotInstalled(installed, up bool) string {
	if installed || up {
		return ""
	}
	return "note-not-installed"
}

func (o OC) StartGateway(ctx context.Context, port int) (string, error) {
	return o.gatewayCmd(ctx, "start", port)
}

func (o OC) StopGateway(ctx context.Context) (string, error) {
	return o.gatewayCmd(ctx, "stop", 0)
}

func (o OC) gatewayCmd(ctx context.Context, verb string, port int) (string, error) {
	if o.CLI == "" {
		return "", fmt.Errorf("openclaw command line not found (set OC_CLI in forgeboard.json)")
	}
	cctx, cancel := context.WithTimeout(ctx, 360*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, "node", o.CLI, "gateway", verb)
	cmd.SysProcAttr = hideCmd
	out, err := cmd.CombinedOutput()
	txt := strings.TrimSpace(string(out))
	if err == nil {
		return txt, nil
	}
	if verb == "start" && port > 0 && waitPortUp(port, 90) {
		return txt, nil // 端口起来了：start 报非零退出不算失败
	}
	return txt, fmt.Errorf("gateway %s failed: %s%s", verb, errText(err, ""), outTail(txt))
}

func StopOneService(ctx context.Context, p Paths, kind string, port int) error {
	if kind == "gateway" {
		oc := NewOC(p)
		if oc.Available() {
			_, err := oc.StopGateway(ctx)
			return err
		}
	}
	return killOnPort(port)
}
