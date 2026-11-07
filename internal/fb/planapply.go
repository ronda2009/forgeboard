package fb

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type PlanError struct {
	Code string            // not_enough_vram | no_visible_card | restart_needed | crash | unknown_model | port_busy
	Args map[string]string // need/free/holder/card/model/running…
	Raw  string            // 引擎/子进程的原始报错行（不翻译，原样显示）
}

func (e *PlanError) Error() string {
	if e.Raw != "" {
		return e.Raw
	}
	return e.Code
}

func setCmdFlags(cmd, args string) string {
	toks := shellSplit(cmd)
	drop := map[string]bool{"-c": true, "-ctk": true, "-ctv": true, "-dev": true, "--device": true}
	var kept []string
	for i := 0; i < len(toks); i++ {
		t := toks[i]
		if drop[t] {
			i++ // 连它的值一起丢
			continue
		}
		if strings.HasPrefix(t, "--device=") || strings.HasPrefix(t, "-c=") {
			continue
		}
		kept = append(kept, t)
	}
	kept = append(kept, shellSplit(args)...)
	return shellJoin(kept)
}

func shellSplit(s string) []string {
	var out []string
	var cur strings.Builder
	inQ := false
	for _, r := range s {
		switch {
		case r == '"':
			inQ = !inQ
		case (r == ' ' || r == '\t' || r == '\n') && !inQ:
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

func shellJoin(toks []string) string {
	for i, t := range toks {
		if strings.ContainsAny(t, " \t\"") && !strings.HasPrefix(t, `"`) {
			toks[i] = `"` + t + `"`
		}
	}
	return strings.Join(toks, " ")
}

func syncSwapCmd(p Paths, id, args string) (bool, error) {
	cfg := filepath.Join(CfgDir(), "swap-config.json")
	b, err := os.ReadFile(cfg)
	if err != nil {
		return false, nil // 没配置文件（引擎自己那份不在配置目录）：不改，照引擎的来
	}
	var doc map[string]any
	if json.Unmarshal(b, &doc) != nil {
		return false, fmt.Errorf("%s 读不出 JSON（改参数前先把它修好）: %w", cfg, err)
	}
	models, _ := doc["models"].(map[string]any)
	entry, _ := models[id].(map[string]any)
	if entry == nil {
		return false, nil // 引擎配置里没这条（外部手写的引擎）：不动别人的配置
	}
	old, _ := entry["cmd"].(string)
	if old == "" {
		return false, nil
	}
	nw := setCmdFlags(old, args)
	if nw == old {
		return false, nil
	}
	entry["cmd"] = nw
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return false, err
	}
	if err := os.WriteFile(cfg, append(out, '\n'), 0o644); err != nil {
		return false, err
	}
	return true, nil
}

func explainStartFail(p Paths, m Model, err error) error {
	raw := strings.TrimSpace(err.Error())
	tail := swapLogTail(60)
	pe := &PlanError{Code: "crash", Args: map[string]string{"model": m.Key}, Raw: raw}
	joined := raw + "\n" + tail
	switch {
	case strings.Contains(joined, "no router for requested model"):
		pe.Code = "unknown_model"
		pe.Raw = orEmpty(tail, raw)
	case strings.Contains(joined, "out of memory") || strings.Contains(joined, "failed to allocate") ||
		strings.Contains(joined, "ggml_cuda_alloc") || strings.Contains(joined, "CUDA error"):
		pe.Code = "not_enough_vram"
		pe.Args["card"] = orNone(firstGPUName())
		pe.Raw = orEmpty(tail, raw)
	case strings.Contains(joined, "address already in use") || strings.Contains(joined, "Only one usage"):
		pe.Code = "port_busy"
		pe.Raw = orEmpty(tail, raw)
	case strings.Contains(joined, "exited prematurely"):
		pe.Code = "crash"
		pe.Raw = orEmpty(tail, raw)
	}
	return pe
}

func swapLogTail(n int) string {
	b, err := os.ReadFile(filepath.Join(CfgDir(), "llama-swap.log"))
	if err != nil {
		return ""
	}
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	var keep []string
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l != "" {
			keep = append(keep, l)
		}
	}
	return strings.Join(keep, "\n")
}

func orEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

func firstGPUName() string {
	gpus, err := GPUs()
	if err != nil || len(gpus) == 0 {
		return ""
	}
	return gpus[0].Name
}

func SwapConfigFingerprint() (hash string, stale bool, err error) {
	cfg := filepath.Join(CfgDir(), "swap-config.json")
	b, err := os.ReadFile(cfg)
	if err != nil {
		return "", false, err
	}
	sum := sha256.Sum256(b)
	hash = hex.EncodeToString(sum[:])
	return hash, !sameCfg(CfgDir(), hash), nil
}
