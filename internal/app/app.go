package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/ronda2009/forgeboard/internal/cli"
	"github.com/ronda2009/forgeboard/internal/fb"
)

type App struct {
	ctx      context.Context
	startTab string
	version  string
	logMu    sync.Mutex
}

var BuildStamp string

var quitting bool

func IsQuitting() bool { return quitting }

type Deps struct {
	StartTab string // 启动先开哪页（调试/自截图用）
	Version  string
}

func New(d Deps) *App {
	return &App{startTab: d.StartTab, version: d.Version}
}

func (a *App) Startup(ctx context.Context) {
	a.ctx = ctx
	if fb.LoadPaths().Lang == "zh" {
		SetUILang("zh")
	}
	a.StartTray(a.TrayIconBytes())
	fb.SetProgress(func(msg string) { a.emit(msg) })
	go a.kickUsageFirstRound()
}

func (a *App) TrayIconBytes() []byte { return trayIconBytes }

func SetTrayIcon(b []byte) { trayIconBytes = b }

var trayIconBytes []byte

var trayTitle = "ForgeBoard"

func SetTrayTitle(s string) {
	if s != "" {
		trayTitle = s
	}
}

func (a *App) HideToTray() { trayHideWindow(a) }

func (a *App) TrayOnClose() bool { return true }

func (a *App) StartTray(iconBytes []byte) {
	if err := startTrayImpl(a, iconBytes); err != nil {
		a.push("tray could not start: " + err.Error() + " (the X button will quit instead of hiding)")
	}
}

func (a *App) TrayReady() bool { return trayAvailable() }

func (a *App) HideMainWindow() {
	if a.ctx != nil {
		runtime.WindowHide(a.ctx)
	}
}

func (a *App) ShowMainWindow() {
	if a.ctx == nil {
		return
	}
	runtime.WindowShow(a.ctx)
	runtime.WindowUnminimise(a.ctx)
}

func (a *App) OpenTab(tab string) {
	a.ShowMainWindow()
	if a.ctx == nil {
		return
	}
	runtime.EventsEmit(a.ctx, "tab", tab)
}

func (a *App) QuitApp() {
	quitting = true
	trayQuitNow()
	if a.ctx != nil {
		runtime.Quit(a.ctx)
	}
}

func (a *App) StartTab() string { return a.startTab }

func (a *App) Stamp() string {
	if BuildStamp == "" {
		return a.version
	}
	return a.version + " · " + buildClock()
}

func buildClock() string {
	if i := strings.Index(BuildStamp, "_"); i >= 0 {
		return BuildStamp[i+1:]
	}
	return BuildStamp
}

func (a *App) SetLang(lang string) string {
	SetUILang(lang)
	a.saveLang(lang)
	a.retitle()
	return lang
}

func (a *App) saveLang(lang string) {
	p := fb.LoadPaths()
	if p.Lang == lang {
		return
	}
	p.Lang = lang
	if b, err := json.MarshalIndent(p, "", "  "); err == nil {
		_ = os.WriteFile(filepath.Join(fb.CfgDir(), "forgeboard.json"), b, 0o644)
	}
}

func (a *App) retitle() {
	if a.ctx == nil {
		return
	}
	t := WindowTitleFor(GetUILang(), a.version)
	runtime.WindowSetTitle(a.ctx, t)
	SetTrayTitle(t)
	trayRefreshTip()
}

func WindowTitleFor(lang, version string) string {
	name := "ForgeBoard"
	if lang == "zh" {
		name = "锻造台"
	}
	if version == "" {
		return name
	}
	return name + " " + version
}

func (a *App) LangNow() string { return GetUILang() }

func GetUILang() string {
	langMu.RLock()
	defer langMu.RUnlock()
	return uiLangGo
}

func (a *App) push(msg string) {
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, "feed", msg)
	}
	fmt.Println(msg)
	a.logMu.Lock()
	defer a.logMu.Unlock()
	if f, err := os.OpenFile(filepath.Join(fb.CfgDir(), "forgeboard-gui.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err == nil {
		fmt.Fprintf(f, "%s %s\n", time.Now().Format("2006-01-02 15:04:05"), msg)
		f.Close()
	}
}

func (a *App) emit(msg string) {
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, "feed", msg)
	}
}

func (a *App) Debug(msg string) { a.push("[frontend] " + msg) }

type GpuRow struct {
	Index int    `json:"index"`
	Name  string `json:"name"`
	Total int    `json:"total"`
	Free  int    `json:"free"`
}

type ModelRow struct {
	Key      string `json:"key"`
	Name     string `json:"name"`
	Need     int    `json:"need"`
	Measured bool   `json:"measured"`
	Engine   string `json:"engine"`
	Port     int    `json:"port"`
	Running  bool   `json:"running"`
	Source   string `json:"source"`
	Fit      string `json:"fit"`               // can | novram | running | switch
	Mounted  string `json:"mounted,omitempty"` // 被 OpenClaw 挂载的哪个模型 id 认领（空＝存着没挂）
	Kind     string `json:"kind,omitempty"`    // embed | vision | text（从挂载清单判，见 mounted.go）
	Tools    bool   `json:"tools,omitempty"`
}

type Snapshot struct {
	Version   string      `json:"version"`
	ConfigDir string      `json:"configDir"`
	Paths     fb.Paths    `json:"paths"`
	GPUs      []GpuRow    `json:"gpus"`
	GpuErr    string      `json:"gpuErr"`
	Router    *RouterInfo `json:"router"`
	Swap      *SwapInfo   `json:"swap"`

	Models    []ModelRow       `json:"models"`
	Now       string           `json:"now"`
	Stamp     string           `json:"stamp"`   // 版本 + 编译时刻（标题栏显示，免得「改了看不到」）
	OCReady   bool             `json:"ocReady"` // OpenClaw 联动能不能用（找不找得到网关命令行）
	OCNote    string           `json:"ocNote"`
	GWPort    int              `json:"gwPort"`             // OpenClaw 网关端口（openclaw.json 现读；界面那个对话入口用它，不写死 18789）
	Windows   []fb.Window      `json:"windows"`            // 各对话窗口当前用哪个模型
	Targets   []fb.ModelCap    `json:"targets"`            // 可切目标模型与上下文上限
	Crons     []fb.Cron        `json:"crons"`              // 定时任务与心跳（网关 cron.list）
	Services  []fb.Service     `json:"services"`           // 服务体检：谁在跑 / 谁没装
	Mounted   []MountedRow     `json:"mounted"`            // OpenClaw 挂载的模型（本地已部署才算，存着没挂的不算）
	Lang      string           `json:"lang"`               // 界面语言（存配置，重启不丢）
	KindNote  string           `json:"kindNote"`           // 「是什么模型」这列怎么判的（一句话说清）
	GPU       string           `json:"gpu"`                // 模型用哪张显卡：auto / 0 / 1（存配置）
	HideSmall bool             `json:"hideSmall"`          // 模型页那个勾选：隐藏嵌入与视觉（存配置）
	HBModel   string           `json:"hbModel"`            // 心跳现在用哪个模型（只读；改它要网关重启）
	CPModel   string           `json:"cpModel"`            // 上下文压缩现在用哪个模型（只读）
	DefModel  string           `json:"defModel"`           // 新窗口的默认模型（只读）
	Versions  []fb.VersionRow  `json:"versions"`           // 本机版本 vs 官方最新
	Host      fb.HostInfo      `json:"host"`               // 整机 CPU 与内存现状
	Powers    []fb.PowerRow    `json:"powers"`             // 每张卡功率上限现状
	Reserve   []fb.ReserveRow  `json:"reserve"`            // 每个档位文件的安全垫现值（只读）
	Cache     fb.CacheInfo     `json:"cache"`              // 大块头缓存现状（只读路由器）
	Startup   []fb.StartupItem `json:"startup"`            // 本机开机启动项（注册表两处 Run + 两个启动文件夹）
	Tasks     []fb.TaskItem    `json:"tasks"`              // 开机计划任务（只展示；含隐藏的与系统那批）
	TaskNote  string           `json:"taskNote,omitempty"` // 读不到/还在读时的说明
	SelfOn    bool             `json:"selfOn"`             // 本程序自己在不在开机启动里
	SelfEntry string           `json:"selfEntry"`          // 在的话那条注册表值
	Usage     *fb.UsageSummary `json:"usage"`
	SelfDir   string           `json:"selfDir"` // 用户启动文件夹路径（给「打开启动文件夹」用）
}

type RouterInfo struct {
	URL     string    `json:"url"`
	Current string    `json:"current"`
	Tiers   []TierRow `json:"tiers"`
	Service string    `json:"service,omitempty"` // 路由器自报的服务名（/health 的 service；界面「×× 监控台」用它）
	Err     string    `json:"err"`
}

type TierRow struct {
	Tier    string `json:"tier"`
	Port    int    `json:"port"`
	Running bool   `json:"running"` // 这一档的端口现在通不通（不照 /__status 的 current）
}

func tierPorts(tiers map[string]int) []int {
	seen := map[int]bool{}
	out := make([]int, 0, len(tiers))
	for _, port := range tiers {
		if port > 0 && !seen[port] {
			seen[port] = true
			out = append(out, port)
		}
	}
	sort.Ints(out)
	return out
}

type SwapInfo struct {
	Port   int      `json:"port"`
	Up     bool     `json:"up"`
	Loaded []string `json:"loaded"`
	Models int      `json:"models"`
	Err    string   `json:"err"`
}

func (a *App) Snapshot() (*Snapshot, error) {
	p := fb.LoadPaths()
	rows := cli.ScanMerge(a.ctx, p)
	cal := fb.AutoCalibrate(a.ctx, p, rows) // 启动/刷新时自动评估
	if cal != nil {
		fb.ApplyCalib(rows)
	}
	s := &Snapshot{
		Version:   a.version,
		ConfigDir: fb.CfgDir(),
		Paths:     p,

		Now:    time.Now().Format("2006-01-02 15:04:05"),
		Models: []ModelRow{},
		GPUs:   []GpuRow{},
		Stamp:  a.Stamp(),
	}
	if gpus, err := fb.GPUs(); err == nil {
		for _, g := range gpus {
			s.GPUs = append(s.GPUs, GpuRow{Index: g.Index, Name: g.Name, Total: g.Total, Free: g.Free})
		}
	} else {
		s.GpuErr = err.Error()
	}
	best, sum, picked, hasPick := 0, 0, 0, false
	for _, g := range s.GPUs {
		sum += g.Free
		if g.Free > best {
			best = g.Free
		}
		if fmt.Sprint(g.Index) == p.GPU {
			picked, hasPick = g.Free, true
		}
	}
	if hasPick {
		best = picked // 指定了显卡：能不能起就按那张卡的空闲算
	}
	bigRunning := false
	for _, m := range rows {
		if m.Running && m.Engine == "strata" {
			bigRunning = true
		}
	}
	for _, m := range rows {
		r := ModelRow{Key: m.Key, Name: m.Name, Need: m.Need, Measured: m.Measured, Engine: m.Engine, Port: m.Port, Running: m.Running, Source: m.Source}
		switch {
		case m.Embed:
			r.Kind = "embed"
		case m.VL:
			r.Kind = "vision"
		default:
			r.Kind = "text"
		}
		limit, dual := best, false
		if m.Engine == "strata" {
			limit, dual = sum, true
		}
		switch {
		case m.Running:
			r.Fit = "running"
		case dual && bigRunning:
			r.Fit = "switch" // 大块头之间是换档，不是叠加（3.x 页① 同逻辑）
		case m.Need == 0:
			r.Fit = "can" // 引擎报的条目没体积信息：不拦
		case m.Need > limit:
			r.Fit = "novram"
		default:
			r.Fit = "can"
		}
		s.Models = append(s.Models, r)
	}
	rurl := p.RouterURL
	rs := fb.AskRouter(a.ctx, rurl)
	if rs == nil {
		if dr := fb.DetectRouter(a.ctx); dr.Value != "" {
			rurl = dr.Value
			rs = fb.AskRouter(a.ctx, rurl)
			p.RouterURL = rurl // 后面几步（服务体检、缓存状态）也读这个地址，一起用现找到的
		}
	}
	if rs != nil {
		up := fb.PortsUp(a.ctx, tierPorts(rs.Tiers))
		ri := &RouterInfo{URL: rurl, Current: rs.Current, Service: fb.RouterService(a.ctx, rurl)}
		for t, port := range rs.Tiers {
			ri.Tiers = append(ri.Tiers, TierRow{Tier: t, Port: port, Running: up[port]})
		}
		sort.Slice(ri.Tiers, func(i, j int) bool { return ri.Tiers[i].Port < ri.Tiers[j].Port })
		if ri.Current != "" {
			port, ok := rs.Tiers[ri.Current]
			if !ok || !up[port] {
				ri.Current = "" // 那一档的端口没人监听 = 服务已停，别报「当前模型 = 它」
			}
		}
		s.Router = ri
	} else {
		s.Router = &RouterInfo{URL: rurl, Err: "no answer"}
	}
	if sw, err := fb.NewSwap(p); err != nil {
		s.Swap = &SwapInfo{Port: p.SwapPort, Err: err.Error()}
	} else {
		si := &SwapInfo{Port: p.SwapPort, Loaded: []string{}}
		if err := sw.Health(a.ctx); err != nil {
			si.Err = "not running"
		} else {
			si.Up = true
			si.Loaded = sw.Running(a.ctx)
			if ms, err := sw.Models(a.ctx); err == nil {
				si.Models = len(ms)
			}
		}
		s.Swap = si
	}
	s.OCReady, s.OCNote = ocStatus(p)
	s.Lang = p.Lang
	s.HideSmall = p.HideSmall
	s.GPU = p.GPU
	oc := fb.NewOC(p)
	if oc.Available() {
		var wg sync.WaitGroup
		var mu sync.Mutex
		wg.Add(3)
		go func() {
			defer wg.Done()
			if ws, err := oc.ListWindows(a.ctx); err == nil {
				mu.Lock()
				s.Windows = ws
				mu.Unlock()
			} else {
				mu.Lock()
				s.OCNote = err.Error()
				mu.Unlock()
			}
		}()
		go func() {
			defer wg.Done()
			if ms, err := oc.ModelCaps(a.ctx); err == nil {
				mu.Lock()
				s.Targets = ms
				mu.Unlock()
			}
		}()
		go func() {
			defer wg.Done()
			if cs, err := oc.ListCrons(a.ctx); err == nil {
				mu.Lock()
				s.Crons = cs
				mu.Unlock()
				a.enrichCronRuns(oc, cs)
			}
		}()
		wg.Wait()
	}
	if ocfg, err := fb.LoadOCConfig(p); err == nil {
		s.GWPort = ocfg.GatewayPort
		fb.MarkMountedRunning(a.ctx, ocfg)
		fb.EnrichKinds(p, ocfg) // 本地 gguf 文件能对上的一律改读文件元数据定类型
		s.Mounted = mountedRows(p, ocfg, s.Models)
		s.Services = fb.Services(a.ctx, p, ocfg)
		s.KindNote = "note-kind"
	} else {
		s.KindNote = "openclaw.json not readable: " + err.Error()
		s.Services = fb.Services(a.ctx, p, nil)
	}
	s.HBModel, s.CPModel, s.DefModel = fb.HeartbeatModels(p)
	s.Versions = a.versionsCached(p)
	s.Host = fb.Host()
	s.Powers = fb.Powers(a.ctx)
	s.Reserve, _ = fb.ReserveNow(p)
	s.Cache = fb.CacheStatus(a.ctx, p)
	s.Startup = fb.StartupList()
	s.Tasks, s.TaskNote = a.tasksCached()
	s.SelfOn, s.SelfEntry = fb.SelfStartState()
	s.SelfDir = fb.StartupFolderUser()
	s.Usage = a.usageCached()
	return s, nil
}

type MountedRow struct {
	Provider string `json:"provider"`
	Host     string `json:"host"` // local | remote
	Port     int    `json:"port,omitempty"`
	ID       string `json:"id"`
	Name     string `json:"name"`
	Ctx      int    `json:"ctx"`
	Running  bool   `json:"running"`
	Kind     string `json:"kind"`               // embed | vision | text
	KindFile string `json:"kindFile,omitempty"` // 模型文件自己元数据判出来的类型（隐藏嵌入/视觉靠它）
	Tools    bool   `json:"tools"`
	Reasoner bool   `json:"reasoner"`
	ActKey   string `json:"actKey,omitempty"` // 起停用的条目 key（对不上就没有起停按钮）
	Fit      string `json:"fit,omitempty"`
	Need     int    `json:"need,omitempty"`
	Engine   string `json:"engine,omitempty"`
	Measured bool   `json:"measured,omitempty"` // 显存数来自实测校准
	GgufName string `json:"ggufName,omitempty"` // 模型自己的名字（文件元数据），界面第二行显示它
}

func mountedRows(p fb.Paths, c *fb.OCConfig, rows []ModelRow) []MountedRow {
	var out []MountedRow
	for _, pr := range c.Providers {
		for _, m := range pr.Models {
			r := MountedRow{Provider: pr.Name, Host: pr.Host(), Port: m.Port, ID: m.ID, Name: m.Name,
				Ctx: m.Ctx, Running: m.Running, Kind: m.Kind, KindFile: m.KindFile, Tools: m.Tools, Reasoner: m.Reasoner,
				GgufName: m.GgufName}
			if hit := matchScan(m.ID, rows); hit != nil {
				r.ActKey, r.Fit, r.Need, r.Engine = hit.Key, hit.Fit, hit.Need, hit.Engine
				hit.Mounted, hit.Kind, hit.Tools = m.ID, m.Kind, m.Tools
				r.Measured = hit.Measured
				if hit.Port != 0 {
					r.Port = hit.Port // 档位真实端口（8092-8095），不是路由器地址 8080
				}
				r.Running = hit.Running
			}
			out = append(out, r)
		}
	}
	_, tierSize := fb.TierModels(p)
	sizeOf := func(r MountedRow) int {
		if v := tierSize[r.ID]; v > 0 {
			return v // 档位：用档位配置引用的 gguf 合计体积（磁盘真实体积，档位之间可比）
		}
		return r.Need // 其它：扫描时按文件体积算的
	}
	sort.Slice(out, func(i, j int) bool {
		if (out[i].Host == "local") != (out[j].Host == "local") {
			return out[i].Host == "local"
		}
		a, b := sizeOf(out[i]), sizeOf(out[j])
		if a != b {
			return a > b
		}
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].ID < out[j].ID
	})
	return out
}

func whereLabel(tok string) string {
	base := strings.TrimPrefix(tok, "where-")
	if base == tok {
		return tok // 不是我们发的 token：原样给，不瞎编
	}
	switch base {
	case "main":
		return uiT("main session")
	case "feishu-group":
		return uiT("feishu group")
	case "feishu-direct":
		return uiT("feishu direct")
	case "webchat":
		return uiT("webchat")
	case "openclaw-weixin":
		return uiT("wechat")
	}
	return uiT(base)
}

func matchScan(id string, rows []ModelRow) *ModelRow {
	for i := range rows {
		k := rows[i].Key
		if k == id || strings.HasSuffix(id, "-"+k) {
			return &rows[i]
		}
	}
	var hit *ModelRow
	n := 0
	for i := range rows {
		if strings.HasPrefix(rows[i].Key, id+"-") {
			hit, n = &rows[i], n+1
		}
	}
	if n == 1 {
		return hit
	}
	return nil
}

func ocStatus(p fb.Paths) (bool, string) {
	if fb.NewOC(p).Available() {
		return true, ""
	}
	return false, "openclaw command line not found (set OC_CLI in forgeboard.json)"
}

func (a *App) SetPowerWatts(index, watts int, dry bool) (string, error) {
	if watts <= 0 && !dry {
		return "", errors.New("no wattage given")
	}
	return fb.SetPower(a.ctx, index, watts, dry)
}

func (a *App) SetPowerElevated(index, watts int) (string, error) {
	return fb.SetPowerElevated(a.ctx, index, watts)
}

func (a *App) RestartGateway(dry bool) (string, error) {
	oc := fb.NewOC(fb.LoadPaths())
	if !oc.Available() {
		return "", errors.New("openclaw command line not found (set OC_CLI in forgeboard.json)")
	}
	if dry {
		return "[DryRun] node openclaw.mjs gateway restart", nil
	}
	out, err := oc.RestartGateway(a.ctx, fb.GatewayPort(fb.LoadPaths()))
	if err != nil {
		return out, err
	}
	return strings.TrimSpace(out), nil
}

func (a *App) ReserveNow() ([]fb.ReserveRow, error) {
	return fb.ReserveNow(fb.LoadPaths())
}

func (a *App) SetReserve(first, later int, dry bool) (*fb.OpResult, error) {
	return fb.ReserveSet(a.ctx, fb.LoadPaths(), first, later, dry)
}

func (a *App) RollbackReserve(dry bool) (*fb.OpResult, error) {
	return fb.ReserveRollback(a.ctx, fb.LoadPaths(), dry)
}

func (a *App) CacheNow() fb.CacheInfo {
	return fb.CacheStatus(a.ctx, fb.LoadPaths())
}

func (a *App) ClearBigCache(dry bool) (*fb.OpResult, error) {
	return fb.ClearCache(a.ctx, fb.LoadPaths(), dry)
}

func (a *App) RestartBigModel(dry bool) (*fb.OpResult, error) {
	return fb.RestartBig(a.ctx, fb.LoadPaths(), dry)
}

func (a *App) RescueGateway(hard, dry bool) (*fb.OpResult, error) {
	return fb.RescueGateway(a.ctx, fb.LoadPaths(), hard, dry)
}

func (a *App) SetHeartbeatModel(model string, dry bool) (*fb.OpResult, error) {
	return fb.SetHeartbeat(fb.LoadPaths(), model, dry)
}

func (a *App) DetectPaths() ([]fb.DetectRow, error) {
	return fb.Detect(a.ctx), nil
}

func (a *App) ApplyDetect() (string, error) {
	rows := fb.Detect(a.ctx)
	p := fb.LoadPaths()
	filled, skipped := 0, 0
	for _, r := range rows {
		if r.Value == "" {
			skipped++
			continue
		}
		switch r.Field {
		case "MODELS":
			p.MODELS = r.Value
		case "LLAMA_EXE":
			p.LLAMA_EXE = r.Value
		case "TIER_DIR":
			p.TIER_DIR = r.Value
		case "PY":
			p.PY = r.Value
		case "SERVE":
			p.SERVE = r.Value
		case "OC_CFG":
			p.OC_CFG = r.Value
		case "OC_HOME":
			p.OC_HOME = r.Value
		case "GW_RESCUE":
			p.GW_RESCUE = r.Value
		case "RouterURL":
			p.RouterURL = r.Value
		default:
			continue
		}
		filled++
	}
	if err := a.SavePaths(p); err != nil {
		return "", err
	}
	return "填了 " + fmt.Sprint(filled) + " 项，没找到跳过 " + fmt.Sprint(skipped) + " 项（写完已读回确认）", nil
}

func (a *App) WindowsNow() ([]fb.Window, error) {
	return fb.NewOC(fb.LoadPaths()).ListWindows(a.ctx)
}

func (a *App) SwitchWindows(model string, keys []string, dry bool) (string, error) {
	if model == "" {
		return "", errors.New("no target model selected")
	}
	oc := fb.NewOC(fb.LoadPaths())
	if !oc.Available() {
		return "", errors.New("openclaw command line not found (set OC_CLI in forgeboard.json)")
	}
	ws, err := oc.ListWindows(a.ctx)
	if err != nil {
		return "", err
	}
	want := map[string]bool{}
	for _, k := range keys {
		want[k] = true
	}
	var hit []fb.Window
	for _, w := range ws {
		if want[w.Key] {
			hit = append(hit, w)
		}
	}
	if len(hit) == 0 {
		return "", fmt.Errorf("no selected window found (refresh first)")
	}
	if dry {
		var sb strings.Builder
		capOf := map[string]int{}
		if ms, err := oc.ModelCaps(a.ctx); err == nil {
			for _, m := range ms {
				capOf[m.Ref] = m.Cap
			}
		}
		newCap := capOf[model]
		for _, w := range hit {
			fmt.Fprintf(&sb, "%s (%s)  %s -> %s", w.Name, whereLabel(w.Where), w.Model, model)
			if newCap > 0 && w.Used > newCap {
				fmt.Fprintf(&sb, "  [!] used %d > new cap %d - compact first", w.Used, newCap)
			}
			sb.WriteString("\n")
		}
		fmt.Fprintf(&sb, "POST sessions.patchMany model=%s targets=%d", model, len(hit))
		return sb.String(), nil
	}
	if err := oc.PatchMany(a.ctx, model, keys); err != nil {
		return "", err
	}
	return fmt.Sprintf(uiT("switched %d window(s) to %s"), len(hit), model), nil
}

func (a *App) StartModel(key string, dry bool) (*fb.OpResult, error) {
	p := fb.LoadPaths()
	rows := cli.ScanMerge(a.ctx, p)
	for _, m := range rows {
		if m.Key == key {
			res, err := fb.StartOp(a.ctx, p, m, dry)
			if res != nil {
				a.push(opLogLine(res)) // 起停的结论落进日志
			}
			return res, err
		}
	}
	return nil, fmt.Errorf("no entry %q (rescan first)", key)
}

func (a *App) ReloadSwap(dry bool) (*fb.OpResult, error) {
	res, err := fb.ReloadSwapOp(a.ctx, fb.LoadPaths(), dry)
	if res != nil {
		a.push(opLogLine(res))
	}
	return res, err
}

func opLogLine(r *fb.OpResult) string {
	if r == nil {
		return ""
	}
	parts := []string{"[op] " + r.Token}
	if r.Dry {
		parts = append(parts, "(dry)")
	}
	for _, k := range sortedKeys(r.Args) {
		parts = append(parts, k+"="+r.Args[k])
	}
	if r.Note != "" {
		parts = append(parts, "note="+oneLine(r.Note))
	}
	return strings.Join(parts, " ")
}

func (a *App) StopModel(key string, dry bool) (*fb.OpResult, error) {
	p := fb.LoadPaths()
	rows := cli.ScanMerge(a.ctx, p)
	for _, m := range rows {
		if m.Key == key {
			res, err := fb.StopOp(a.ctx, p, m, dry)
			if res != nil {
				a.push(opLogLine(res))
			}
			return res, err
		}
	}
	return nil, fmt.Errorf("no entry %q (rescan first)", key)
}

func sortedKeys(m map[string]string) []string {
	var ks []string
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func (a *App) StopAll(dry bool) (*fb.OpResult, error) {
	return fb.StopAll(a.ctx, fb.LoadPaths(), dry, false)
}

func (a *App) StopEverything(dry bool) (*fb.OpResult, error) {
	return fb.StopAll(a.ctx, fb.LoadPaths(), dry, true)
}

func (a *App) ServiceAct(kind string, port int, act string, dry bool) (*fb.OpResult, error) {
	return fb.ControlService(a.ctx, fb.LoadPaths(), kind, port, act, dry)
}

// SetGPU 记住用哪张显卡，并按它重写换模型器配置（不重启服务，等下次加载生效）。
func (a *App) SetGPU(g string) string {
	if g != "auto" {
		digits := len(g) > 0
		for _, ch := range g {
			if ch < '0' || ch > '9' {
				digits = false
				break
			}
		}
		if !digits {
			g = "auto"
		}
	}
	p := fb.LoadPaths()
	if p.GPU != g {
		p.GPU = g
		if b, err := json.MarshalIndent(p, "", "  "); err == nil {
			_ = os.WriteFile(filepath.Join(fb.CfgDir(), "forgeboard.json"), b, 0o644)
		}
	}
	_, _, _ = fb.WriteSwapConfig(p, true)
	return g
}

func (a *App) SetHideSmall(hide bool) bool {
	p := fb.LoadPaths()
	if p.HideSmall != hide {
		p.HideSmall = hide
		if b, err := json.MarshalIndent(p, "", "  "); err == nil {
			_ = os.WriteFile(filepath.Join(fb.CfgDir(), "forgeboard.json"), b, 0o644)
		}
	}
	return hide
}

func (a *App) versionsCached(p fb.Paths) []fb.VersionRow {
	versionMu.Lock()
	defer versionMu.Unlock()
	if len(versionCache) > 0 && time.Since(versionAt) < 10*time.Minute {
		return versionCache
	}
	versionCache = fb.Versions(a.ctx, p, a.version)
	versionAt = time.Now()
	return versionCache
}

var (
	versionMu    sync.Mutex
	versionCache []fb.VersionRow
	versionAt    time.Time
)

func (a *App) OpenLink(raw string) error {
	u := strings.TrimSpace(raw)
	if u == "" {
		return errors.New("no address to open")
	}
	if !strings.HasPrefix(u, "http://127.0.0.1:") && !strings.HasPrefix(u, "http://localhost:") {
		return errors.New("only a local loopback address may be opened from here")
	}
	runtime.BrowserOpenURL(a.ctx, u)
	return nil
}

func (a *App) DoctorText() string {
	var sb strings.Builder
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	_ = cli.CmdDoctor(a.ctx)
	w.Close()
	os.Stdout = old
	buf := make([]byte, 1<<16)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			sb.Write(buf[:n])
		}
		if err != nil {
			break
		}
	}
	return sb.String()
}

func (a *App) SavePaths(p fb.Paths) error {
	old := fb.LoadPaths()
	if p.Lang == "" {
		p.Lang = old.Lang
	}
	if p.GW_RESCUE == "" {
		p.GW_RESCUE = old.GW_RESCUE
	}
	if !p.HideSmall {
		p.HideSmall = old.HideSmall
	}
	if p.RouterURL == "" {
		p.RouterURL = "http://127.0.0.1:8080"
	}
	if p.SwapPort == 0 {
		p.SwapPort = 8089
	}
	b, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	dst := filepath.Join(fb.CfgDir(), "forgeboard.json")
	if err := os.WriteFile(dst, b, 0o644); err != nil {
		return err
	}
	back := fb.LoadPaths()
	if back.MODELS != p.MODELS || back.LLAMA_EXE != p.LLAMA_EXE || back.SwapPort != p.SwapPort {
		return fmt.Errorf("written but read-back differs (check the file): %s", dst)
	}
	return nil
}

func dryText(p fb.Paths, m fb.Model, start bool) string {
	var sb strings.Builder
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	if start {
		_ = fb.Start(context.Background(), p, m, true)
	} else {
		_ = fb.Stop(context.Background(), p, m, true)
	}
	w.Close()
	os.Stdout = old
	buf := make([]byte, 1<<16)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			sb.Write(buf[:n])
		}
		if err != nil {
			break
		}
	}
	return strings.TrimSpace(sb.String())
}
