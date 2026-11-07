package fb

//

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/abrander/gguf"
	goproc "github.com/shirou/gopsutil/v3/process"
	"github.com/ronda2009/forgeboard/internal/budget"
)

func GGUFMeta(path string) (m budget.Meta, ok bool) {
	st, err := os.Stat(path)
	if err != nil {
		return m, false
	}
	r, err := gguf.OpenFile(path)
	if err != nil {
		return m, false
	}
	defer r.Close()
	m.FileMiB = int(st.Size() / (1024 * 1024))
	m.Name = filepath.Base(path)
	arch, _ := metaStr(r.Metadata, "general.architecture")
	if arch == "" {
		return m, false
	}
	get := func(suffix string) (int, bool) { return metaInt(r.Metadata, arch+"."+suffix) }
	if m.Layers, ok = get("block_count"); !ok {
		return m, false
	}
	if m.KVHeads, ok = get("attention.head_count_kv"); !ok {
		return m, false
	}
	kl, okk := get("attention.key_length")
	vl, okv := get("attention.value_length")
	switch {
	case okk && okv:
		m.KVDim = kl
		if vl > m.KVDim {
			m.KVDim = vl
		}
	case okk:
		m.KVDim = kl
	default:
		return m, false
	}
	if m.TrainCtx, ok = get("context_length"); !ok {
		return m, false
	}
	m.FullIntval, _ = get("full_attention_interval") // 没这个键＝每层都是全注意力，0 是对的
	m.HasMMProj = findMMProj(filepath.Dir(path), filepath.Base(path)) != ""
	return m, m.Ok()
}

func metaInt(md map[string]any, key string) (int, bool) {
	v, ok := md[key]
	if !ok {
		return 0, false
	}
	switch n := v.(type) {
	case uint8:
		return int(n), true
	case uint16:
		return int(n), true
	case uint32:
		return int(n), true
	case uint64:
		return int(n), true
	case int8:
		return int(n), true
	case int16:
		return int(n), true
	case int32:
		return int(n), true
	case int64:
		return int(n), true
	case int:
		return n, true
	case float32:
		return int(n), true
	case float64:
		return int(n), true
	}
	return 0, false
}

func metaStr(md map[string]any, key string) (string, bool) {
	v, ok := md[key]
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	if !ok || s == "" {
		return "", false
	}
	return s, true
}

//
func Cards(llamaExe string) []budget.Card {
	out, err := exec.Command("nvidia-smi",
		"--query-gpu=index,uuid,name,memory.total,memory.free", "--format=csv,noheader,nounits").Output()
	if err != nil {
		return nil
	}
	var cards []budget.Card
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		f := strings.Split(line, ",")
		if len(f) < 5 {
			continue
		}
		total, _ := strconv.Atoi(strings.TrimSpace(f[3]))
		free, _ := strconv.Atoi(strings.TrimSpace(f[4]))
		cards = append(cards, budget.Card{
			UUID: strings.TrimSpace(f[1]), Name: strings.TrimSpace(f[2]),
			TotalMiB: total, FreeMiB: free,
		})
	}
	if len(cards) == 0 {
		return nil
	}
	holdCards(cards)
	labelCards(llamaExe, cards)
	return cards
}

func holdCards(cards []budget.Card) {
	out, err := exec.Command("nvidia-smi", "--query-compute-apps=pid,gpu_uuid", "--format=csv,noheader").Output()
	if err != nil {
		return
	}
	byUUID := map[string]*budget.Card{}
	for i := range cards {
		byUUID[cards[i].UUID] = &cards[i]
	}
	seen := map[string]bool{}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		f := strings.Split(line, ",")
		if len(f) < 2 {
			continue
		}
		c := byUUID[strings.TrimSpace(f[1])]
		if c == nil {
			continue
		}
		name := procName(strings.TrimSpace(f[0]))
		if name == "" || seen[c.UUID+name] {
			continue
		}
		seen[c.UUID+name] = true
		if c.Holder == "" {
			c.Holder = name
		} else if !strings.Contains(c.Holder, name) {
			c.Holder += "+" + name
		}
	}
}

func procName(raw string) string {
	pid, err := strconv.Atoi(raw)
	if err != nil || pid <= 0 {
		return ""
	}
	pr, err := goproc.NewProcess(int32(pid))
	if err != nil {
		return ""
	}
	name, err := pr.Name()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(name)
}

//
func labelCards(llamaExe string, cards []budget.Card) {
	if llamaExe == "" {
		return
	}
	out, err := exec.Command(llamaExe, "--list-devices").Output()
	if err != nil {
		return // 引擎没装/跑不了：不猜设备名，后面按「没有可见卡」判
	}
	type dev struct {
		label, name string
		total       int
	}
	var devs []dev
	for _, line := range strings.Split(string(out), "\n") {
		l := strings.TrimSpace(line)
		i := strings.Index(l, ":")
		if i <= 0 {
			continue
		}
		head, rest := l[:i], l[i+1:]
		name := rest
		if j := strings.Index(name, "("); j >= 0 {
			name = name[:j] // 括号里是「24575 MiB, 23303 MiB free」，不是卡名
		}
		d := dev{label: strings.TrimSpace(head), name: normGPUName(name)}
		if tm := reDevTotal.FindStringSubmatch(rest); len(tm) > 1 {
			d.total, _ = strconv.Atoi(tm[1])
		}
		if d.label == "" || d.name == "" {
			continue
		}
		devs = append(devs, d)
	}
	if len(devs) == 0 {
		return
	}
	used := map[string]bool{}
	for i := range cards {
		cn := normGPUName(cards[i].Name)
		best, bestGap := "", 0
		for _, d := range devs {
			if used[d.label] || d.name != cn {
				continue
			}
			gap := 1 << 30
			if d.total > 0 && cards[i].TotalMiB > 0 {
				gap = d.total - cards[i].TotalMiB
				if gap < 0 {
					gap = -gap
				}
			}
			if best == "" || gap < bestGap {
				best, bestGap = d.label, gap
			}
		}
		if best != "" {
			cards[i].Label, cards[i].Visible = best, true
			used[best] = true
		}
	}
}

var reDevTotal = regexp.MustCompile(`\((\d+)\s*MiB`)

func normGPUName(s string) string {
	s = strings.ToLower(s)
	for _, junk := range []string{"nvidia ", "geforce ", "rtx ", " (", "(", ")"} {
		s = strings.ReplaceAll(s, junk, " ")
	}
	return strings.Join(strings.Fields(s), "")
}

func ModelGGUF(p Paths, m Model) string {
	if m.GGUF != "" {
		cand := m.GGUF
		if !filepath.IsAbs(cand) {
			cand = filepath.Join(p.MODELS, cand)
		}
		if _, err := os.Stat(cand); err == nil {
			return cand
		}
	}
	if cmd := SwapCmdOf(p, m.Key); cmd != "" {
		if f := reCmdModel.FindStringSubmatch(cmd); len(f) > 1 {
			if _, err := os.Stat(f[1]); err == nil {
				return f[1]
			}
		}
	}
	return ""
}

var reCmdModel = regexp.MustCompile(`(?:^|\s)-m\s+"([^"]+\.gguf)"|(?:^|\s)-m\s+([^\s"]+\.gguf)`)

func LogAction(msg string) {
	f, err := os.OpenFile(filepath.Join(CfgDir(), "forgeboard-gui.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s %s\n", time.Now().Format("2006-01-02 15:04:05"), msg)
}

func PlanStart(p Paths, ggufPath string) (budget.Plan, string) {
	if ggufPath == "" {
		return budget.Plan{Code: "no_meta", Reason: "this entry has no gguf file (router tier): the engine's own config decides its parameters", Args: map[string]string{"model": "(router tier)"}}, ""
	}
	m, ok := GGUFMeta(ggufPath)
	if !ok {
		return budget.Plan{Code: "no_meta", Reason: fmt.Sprintf("cannot read the header of %s (missing GGUF fields or a broken file): keeping the parameters from the config file", filepath.Base(ggufPath)), Args: map[string]string{"model": filepath.Base(ggufPath)}}, ""
	}
	pl := budget.Solve(m, Cards(p.LLAMA_EXE))
	if pl.Code != "" {
		return pl, ""
	}
	args := fmt.Sprintf("-c %d -ctk %s -ctv %s", pl.Ctx, pl.KV, pl.KV)
	if pl.Card.Label != "" {
		args += " --device " + pl.Card.Label
	}
	return pl, args
}
