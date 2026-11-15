package fb

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const calibMargin = 512 // 实测值 + 512 MiB 余量当门槛

type Calib struct {
	Used map[string]int    `json:"used"` // 模型 key → 实测占用 MiB
	Src  map[string]string `json:"src"`  // 模型 key → 量它时那个模型的标识；对不上说明模型换过了，旧实测作废
}

func calibFile() string { return filepath.Join(CfgDir(), "forgeboard-calib.json") }

func LoadCalib() Calib {
	c := Calib{Used: map[string]int{}, Src: map[string]string{}}
	b, err := os.ReadFile(calibFile())
	if err != nil {
		return c
	}
	_ = json.Unmarshal(b, &c)
	if c.Used == nil {
		c.Used = map[string]int{}
	}
	if c.Src == nil {
		c.Src = map[string]string{}
	}
	return c
}

func saveCalib(c Calib) {
	if c.Used == nil {
		c.Used = map[string]int{}
	}
	if c.Src == nil {
		c.Src = map[string]string{}
	}
	b, _ := json.MarshalIndent(c, "", "  ")
	_ = os.WriteFile(calibFile(), b, 0o644)
}

// 有实测用实测（+512 余量），没实测用体积估算 - 不写死任何表。
// 模型换过（标识对不上）就不算实测：宁可显示估算，也不给新模型顶个旧数字。
func ApplyCalib(rows []Model) {
	c := LoadCalib()
	var tierSize map[string]int
	backfill := 0
	for i := range rows {
		v, ok := c.Used[rows[i].Key]
		if !ok || v <= 0 {
			continue
		}
		if tierSize == nil {
			_, tierSize = TierModels(LoadPaths())
		}
		want := calibSrc(&rows[i], tierSize)
		if c.Src[rows[i].Key] == "" && want != "" {
			c.Src[rows[i].Key] = want // 老记录没存过标识：认下来，顺手补上，下次就能比了
			backfill++
		} else if want != "" && c.Src[rows[i].Key] != want {
			continue // 模型文件换过了：旧实测作废
		}
		rows[i].Need = v + calibMargin
		rows[i].Measured = true
	}
	if backfill > 0 {
		saveCalib(c)
	}
}

// 这个模型现在的标识：文件名+体积（gguf）或档位配置里的合计体积（档位）。算不出来就回空（不校验）。
func calibSrc(m *Model, tierSize map[string]int) string {
	if m.GGUF != "" {
		cand := m.GGUF
		if !filepath.IsAbs(cand) {
			cand = filepath.Join(LoadPaths().MODELS, cand)
		}
		if st, err := os.Stat(cand); err == nil {
			return filepath.Base(m.GGUF) + ":" + itoa(int(st.Size()>>20))
		}
		return ""
	}
	if m.Tier != "" {
		if v := tierSize[m.Tier]; v > 0 {
			return "tier:" + m.Tier + ":" + itoa(v)
		}
	}
	return ""
}

type CalibStats struct {
	Count int      `json:"count"`
	Keys  []string `json:"keys"`
}

func CalibStatsNow() CalibStats {
	c := LoadCalib()
	s := CalibStats{Keys: []string{}}
	for k, v := range c.Used {
		if v > 0 {
			s.Keys = append(s.Keys, k)
			s.Count++
		}
	}
	sort.Strings(s.Keys)
	return s
}

func CalibStatsLine() string {
	s := CalibStatsNow()
	if s.Count == 0 {
		return "no measurements yet; using size estimates"
	}
	return itoa(s.Count) + " measured model(s): " + strings.Join(s.Keys, ", ")
}
