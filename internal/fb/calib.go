package fb

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

const calibMargin = 512 // 沿用 3.x：实测值 + 512 MiB 余量当门槛

type Calib struct {
	Baseline int            `json:"baseline"` // 引擎都没跑时全卡占用（桌面/系统）
	Used     map[string]int `json:"used"`     // 模型 key → 实测占用 MiB
}

func calibFile() string { return filepath.Join(CfgDir(), "forgeboard-calib.json") }

func LoadCalib() Calib {
	c := Calib{Used: map[string]int{}}
	b, err := os.ReadFile(calibFile())
	if err != nil {
		return c
	}
	_ = json.Unmarshal(b, &c)
	if c.Used == nil {
		c.Used = map[string]int{}
	}
	return c
}

func saveCalib(c Calib) {
	if c.Used == nil {
		c.Used = map[string]int{}
	}
	b, _ := json.MarshalIndent(c, "", "  ")
	_ = os.WriteFile(calibFile(), b, 0o644)
}

func totalUsedMiB() (int, bool) {
	gpus, err := GPUs()
	if err != nil {
		return 0, false
	}
	sum := 0
	for _, g := range gpus {
		sum += g.Total - g.Free
	}
	if sum <= 0 {
		return 0, false
	}
	return sum, true
}

//
type CalibResult struct {
	Token string            `json:"token"` // model / baseline（界面查文案时加 cal_ 前缀）
	Args  map[string]string `json:"args"`  // model/used/total/baseline
}

func calibRes(token string, kv ...string) *CalibResult {
	r := &CalibResult{Token: token, Args: map[string]string{}}
	for i := 0; i+1 < len(kv); i += 2 {
		r.Args[kv[i]] = kv[i+1]
	}
	return r
}

func Calibrate(ctx context.Context, p Paths, runningKey string) (*CalibResult, error) {
	used, ok := totalUsedMiB()
	if !ok {
		return nil, errors.New("nvidia-smi unavailable (no NVIDIA GPU?)")
	}
	c := LoadCalib()
	if runningKey == "" {
		if p.RouterURL == "" {
			return nil, nil // 引擎地址都没配：不知道是不是真没跑，不瞎记基线
		}
		if rs := AskRouter(ctx, p.RouterURL); rs != nil {
			return nil, nil // 引擎在跑但认不出哪个模型：不记（不编数）
		}
		c.Baseline = used
		saveCalib(c)
		return calibRes("baseline", "used", itoa(used)), nil
	}
	val := used - c.Baseline
	if val <= 0 {
		val = used // 基线还没量过：先按总占用记（偏保守，不会误判能起）
	}
	c.Used[runningKey] = val
	saveCalib(c)
	return calibRes("model", "model", runningKey, "used", itoa(val), "total", itoa(used), "baseline", itoa(c.Baseline)), nil
}

func AutoCalibrate(ctx context.Context, p Paths, rows []Model) *CalibResult {
	var running []Model
	for _, m := range rows {
		if m.Running && m.Engine != "other" {
			running = append(running, m)
		}
	}
	if len(running) > 1 {
		return nil // 多个同时在跑归不到一个模型头上 - 不编数
	}
	key := ""
	if len(running) == 1 {
		key = running[0].Key
	}
	res, err := Calibrate(ctx, p, key)
	if err != nil {
		return nil
	}
	return res
}

func RecordRun(ctx context.Context, key string) {
	used, ok := totalUsedMiB()
	if !ok {
		return
	}
	c := LoadCalib()
	val := used - c.Baseline
	if val <= 0 {
		val = used
	}
	c.Used[key] = val
	saveCalib(c)
}

func ApplyCalib(rows []Model) {
	c := LoadCalib()
	for i := range rows {
		if v, ok := c.Used[rows[i].Key]; ok && v > 0 {
			rows[i].Need = v + calibMargin
			rows[i].Measured = true
		}
	}
}

type CalibStats struct {
	Count    int      `json:"count"`
	Baseline int      `json:"baseline"`
	Keys     []string `json:"keys"`
}

func CalibLine(r *CalibResult) string {
	if r == nil {
		return ""
	}
	if r.Token == "baseline" {
		return "recorded desktop baseline = " + r.Args["used"] + " MiB (no model running)"
	}
	return "recorded " + r.Args["model"] + " = " + r.Args["used"] + " MiB (total " + r.Args["total"] +
		" - baseline " + r.Args["baseline"] + ")"
}

func CalibStatsNow() CalibStats {
	c := LoadCalib()
	s := CalibStats{Baseline: c.Baseline, Keys: []string{}}
	for k := range c.Used {
		s.Keys = append(s.Keys, k)
		s.Count++
	}
	return s
}

func CalibStatsLine() string {
	s := CalibStatsNow()
	if s.Count == 0 {
		return "no measurements yet (baseline " + itoa(s.Baseline) + "); using size estimates"
	}
	return itoa(s.Count) + " measured model(s), baseline " + itoa(s.Baseline) + " MiB: " + strings.Join(s.Keys, ", ")
}
