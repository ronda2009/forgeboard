package fb

import (
	"context"
	"strings"

	goproc "github.com/shirou/gopsutil/v3/process"
)

// 在跑的模型：按「监听这个端口的进程（含它的子进程）」实测显存。
// 谁起的都能量到（路由器换档、llama-swap、外部脚本都一样），多个模型同时在跑也各归各的。
// 量完写进校准表，界面据此显示「实测」；量不到的不写，宁可显示估算也不给假实测。
func MeasureRunning(rows []Model) {
	any := false
	for _, m := range rows {
		if m.Running && m.Port > 0 {
			any = true
			break
		}
	}
	if !any {
		return
	}
	vram, ok := procVRAMCached(false)
	if !ok {
		return // 系统统计取不到：不动校准表
	}
	c := LoadCalib()
	_, tierSize := TierModels(LoadPaths())
	changed := false
	for i := range rows {
		m := &rows[i]
		if !m.Running || m.Port <= 0 {
			continue
		}
		v, found := attribVRAM(m, vram)
		if !found {
			continue // 认不准：留着上次的数
		}
		if v <= 0 {
			// 进程找到了但一点显存没占（比如纯 CPU 跑）：这条实测数不可信，删掉退回估算
			if _, had := c.Used[m.Key]; had {
				delete(c.Used, m.Key)
				changed = true
			}
			continue
		}
		m.Need = v + calibMargin
		m.Measured = true
		if diff := v - c.Used[m.Key]; diff > 256 || diff < -256 { // 只在大变化时写盘，免得一刷新就改数
			c.Used[m.Key] = v
			changed = true
		}
		if src := calibSrc(m, tierSize); src != "" && c.Src[m.Key] != src {
			c.Src[m.Key] = src // 记下这次量的是哪个模型文件
			changed = true
		}
	}
	if changed {
		saveCalib(c)
	}
}

// 把显存归到这一个模型头上：先找监听端口的进程，再顺子进程找。
// llama-swap 可能同时挂着好几个模型，所以带模型文件名的一律按命令行认领。
func attribVRAM(m *Model, vram map[int]int) (int, bool) {
	pid := int(listeningPID(m.Port))
	if pid <= 0 {
		return 0, false
	}
	pids := treePIDs(pid, 4)
	if m.GGUF == "" {
		// 档位/外部引擎：整棵进程树都是这一个模型的（引擎本体 + 它自己的视觉助手）
		if strings.Contains(strings.ToLower(procExe(int32(pid))), "llama-swap") {
			return 0, false // 换模型器同时管好几个模型，认不到文件名就不归
		}
		sum := 0
		for _, p := range pids {
			sum += vram[p]
		}
		return sum, true
	}
	needle := strings.ToLower(m.GGUF)
	sum, found := 0, false
	for _, p := range pids {
		cl, err := procCmdline(p)
		if err != nil || !strings.Contains(strings.ToLower(cl), needle) {
			continue
		}
		if v, has := vram[p]; has {
			sum += v
			found = true
		}
	}
	return sum, found
}

// 路由器档位实际在听的端口（换档后端口可能变）。
func tierPort(ctx context.Context, p Paths, m Model) int {
	if rs := AskRouter(ctx, p.RouterURL); rs != nil {
		if port, ok := rs.Tiers[tierNameOf(m)]; ok {
			return port
		}
	}
	return 0
}

// 起完立刻量一次（模型已加载完，数最准）。port = 这个模型实际在听的端口。
func measureAfterStart(p Paths, m Model, port int) {
	if port <= 0 {
		port = m.Port
	}
	m.Running = true
	m.Port = port
	if m.GGUF == "" {
		m.GGUF = ModelGGUF(p, m)
	}
	invalidateVRAMCache() // 刚起完：要拿新数，不能用旧缓存
	MeasureRunning([]Model{m})
}

func procCmdline(pid int) (string, error) {
	pr, err := goproc.NewProcess(int32(pid))
	if err != nil {
		return "", err
	}
	return pr.Cmdline()
}

func treePIDs(pid int, depth int) []int {
	out := []int{pid}
	if depth <= 0 {
		return out
	}
	pr, err := goproc.NewProcess(int32(pid))
	if err != nil {
		return out
	}
	kids, err := pr.Children()
	if err != nil {
		return out
	}
	for _, k := range kids {
		out = append(out, treePIDs(int(k.Pid), depth-1)...)
	}
	return out
}
