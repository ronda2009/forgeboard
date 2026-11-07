package fb

//

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

type UsageDaily struct {
	Date      string  `json:"date"`
	Tokens    int64   `json:"tokens"`
	Cost      float64 `json:"cost"`
	Messages  int     `json:"messages"`
	ToolCalls int     `json:"toolCalls"`
	Errors    int     `json:"errors"`
}

type UsageGroup struct {
	Provider string  `json:"provider"`
	Model    string  `json:"model,omitempty"`
	Count    int     `json:"count"`
	Tokens   int64   `json:"tokens"`
	Cost     float64 `json:"cost"`
	Unpriced int     `json:"unpriced"` // 没有单价的条目数（本地模型都是 0 元，这不是坏了）
}

type UsageModelDay struct {
	Date     string  `json:"date"`
	Provider string  `json:"provider"`
	Model    string  `json:"model"`
	Tokens   int64   `json:"tokens"`
	Cost     float64 `json:"cost"`
	Count    int     `json:"count"`
}

type UsageModelRoll struct {
	Provider string  `json:"provider"`
	Model    string  `json:"model"`
	Today    int64   `json:"today"`
	D7       int64   `json:"d7"`
	D30      int64   `json:"d30"`
	Cost     float64 `json:"cost"`
	Paid     bool    `json:"paid"` // 真花过钱（本地模型没有单价，这里是 false）
	Calls    int     `json:"calls"`
	Local    bool    `json:"local"` // 本机跑的模型（openclaw.json 里 baseUrl 指 127.0.0.1）＝不花钱
}

type UsageRange struct {
	Tokens int64   `json:"tokens"`
	Cost   float64 `json:"cost"`
}

type UsageTotals struct {
	Input     int64   `json:"input"`
	Output    int64   `json:"output"`
	CacheRead int64   `json:"cacheRead"`
	Tokens    int64   `json:"tokens"`
	Cost      float64 `json:"cost"`
	Unpriced  int     `json:"unpriced"`
}

type UsageSummary struct {
	At        int64            `json:"at"` // 这份数据什么时候采的
	AgeSec    int64            `json:"ageSec"`
	Source    string           `json:"source"` // file | live | cache
	Cache     string           `json:"cache"`  // 网关那边缓存状态 fresh | refreshing
	Start     string           `json:"start"`
	End       string           `json:"end"`
	Totals    UsageTotals      `json:"totals"`
	Messages  int              `json:"messages"`
	ToolCalls int              `json:"toolCalls"`
	Errors    int              `json:"errors"`
	Windows   int              `json:"windows"` // 窗口总数（列表可能截断，这里给真数）
	Daily     []UsageDaily     `json:"daily"`
	Today     UsageRange       `json:"today"`  // 本机今天
	D7        UsageRange       `json:"d7"`     // 最近 7 天（含今天）
	Models    []UsageModelRoll `json:"models"` // 一个模型一行
	ByModel   []UsageGroup     `json:"byModel"`
	ByService []UsageGroup     `json:"byService"`
	Note      string           `json:"note,omitempty"` // 采不到时的原因（文案键，界面翻译）
	Rate      float64          `json:"rate,omitempty"` // 美元兑人民币（网关报价是美元；抓不到用兜底值）
}

func usageFile() string { return filepath.Join(CfgDir(), "openclaw-usage.json") }

const usageLimit = 500

func usageParams() map[string]any {
	return map[string]any{"limit": usageLimit, "mode": "specific", "utcOffset": localUTCOffset()}
}

func localUTCOffset() string {
	_, offset := time.Now().Zone()
	sign := "+"
	if offset < 0 {
		sign = "-"
		offset = -offset
	}
	return fmt.Sprintf("UTC%s%d:%02d", sign, offset/3600, (offset%3600)/60)
}

func FetchUsage(ctx context.Context, p Paths) (*UsageSummary, error) {
	oc := NewOC(p)
	if oc.CLI == "" {
		return nil, errors.New("usage_no_cli")
	}
	params, perr := json.Marshal(usageParams())
	if perr != nil {
		return nil, perr
	}
	cctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, "node", oc.CLI, "gateway", "call", "sessions.usage", "--params", string(params), "--timeout", "80000", "--json")
	if runtime.GOOS == "windows" {
		cmd.SysProcAttr = hideCmd
	}
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok && len(ee.Stderr) > 0 {
			return nil, fmt.Errorf("%w: %s", err, trimTail(string(ee.Stderr), 200))
		}
		return nil, err
	}
	return parseUsage(out, usageLocalProviders(p), fetchUsageRate())
}

func parseUsage(out []byte, localProv map[string]bool, rate float64) (*UsageSummary, error) {
	var raw struct {
		StartDate string `json:"startDate"`
		EndDate   string `json:"endDate"`
		CacheStat struct {
			Status string `json:"status"`
		} `json:"cacheStatus"`
		Totals struct {
			Input     int64   `json:"input"`
			Output    int64   `json:"output"`
			CacheRead int64   `json:"cacheRead"`
			Tokens    int64   `json:"totalTokens"`
			Cost      float64 `json:"totalCost"`
			Unpriced  int     `json:"missingCostEntries"`
		} `json:"totals"`
		Sessions []struct{} `json:"sessions"`
		Aggregates struct {
			Messages struct {
				Total     int `json:"total"`
				ToolCalls int `json:"toolCalls"`
				Errors    int `json:"errors"`
			} `json:"messages"`
			Daily []struct {
				Date      string  `json:"date"`
				Tokens    int64   `json:"tokens"`
				Cost      float64 `json:"cost"`
				Messages  int     `json:"messages"`
				ToolCalls int     `json:"toolCalls"`
				Errors    int     `json:"errors"`
			} `json:"daily"`
			ByModel []struct {
				Provider string  `json:"provider"`
				Model    string  `json:"model"`
				Count    int     `json:"count"`
				Tokens   int64   `json:"totalTokens"`
				Cost     float64 `json:"totalCost"`
				Unpriced int     `json:"missingCostEntries"`
			} `json:"byModel"`
			ByProvider []struct {
				Provider string  `json:"provider"`
				Count    int     `json:"count"`
				Tokens   int64   `json:"totalTokens"`
				Cost     float64 `json:"totalCost"`
				Unpriced int     `json:"missingCostEntries"`
			} `json:"byProvider"`
			ModelDaily []struct {
				Date     string  `json:"date"`
				Provider string  `json:"provider"`
				Model    string  `json:"model"`
				Tokens   int64   `json:"tokens"`
				Cost     float64 `json:"cost"`
				Count    int     `json:"count"`
			} `json:"modelDaily"`
		} `json:"aggregates"`
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("usage json unreadable: %w", err)
	}
	s := &UsageSummary{
		At:      time.Now().UnixMilli(),
		Cache:   raw.CacheStat.Status,
		Start:   raw.StartDate,
		End:     raw.EndDate,
		Source:  "live",
		Windows: len(raw.Sessions),
		Totals: UsageTotals{
			Input: raw.Totals.Input, Output: raw.Totals.Output, CacheRead: raw.Totals.CacheRead,
			Tokens: raw.Totals.Tokens, Cost: raw.Totals.Cost, Unpriced: raw.Totals.Unpriced,
		},
		Messages:  raw.Aggregates.Messages.Total,
		ToolCalls: raw.Aggregates.Messages.ToolCalls,
		Errors:    raw.Aggregates.Messages.Errors,
	}
	if rate <= 0 {
		rate = usageRateDefault
	}
	s.Rate = rate

	for _, d := range raw.Aggregates.Daily {
		s.Daily = append(s.Daily, UsageDaily{Date: d.Date, Tokens: d.Tokens, Cost: d.Cost, Messages: d.Messages, ToolCalls: d.ToolCalls, Errors: d.Errors})
	}
	if len(s.Daily) > 31 {
		s.Daily = s.Daily[len(s.Daily)-31:]
	}
	for _, m := range raw.Aggregates.ByModel {
		s.ByModel = append(s.ByModel, UsageGroup{Provider: m.Provider, Model: m.Model, Count: m.Count, Tokens: m.Tokens, Cost: m.Cost, Unpriced: m.Unpriced})
	}
	for _, m := range raw.Aggregates.ByProvider {
		s.ByService = append(s.ByService, UsageGroup{Provider: m.Provider, Count: m.Count, Tokens: m.Tokens, Cost: m.Cost, Unpriced: m.Unpriced})
	}
	today, d7from := localToday()
	for _, d := range s.Daily {
		if d.Date >= d7from {
			s.D7.Tokens += d.Tokens
			s.D7.Cost += d.Cost
		}
		if d.Date == today {
			s.Today.Tokens += d.Tokens
			s.Today.Cost += d.Cost
		}
	}
	var mdays []UsageModelDay
	for _, m := range raw.Aggregates.ModelDaily {
		mdays = append(mdays, UsageModelDay{Date: m.Date, Provider: m.Provider, Model: m.Model, Tokens: m.Tokens, Cost: m.Cost, Count: m.Count})
	}
	s.Models = rollUsageModels(mdays, today, d7from, localProv)
	return s, nil
}

func WriteUsage(s *UsageSummary) error {
	if s.Totals.Tokens == 0 && s.Windows > 0 {
		return errors.New("usage_empty")
	}
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	return os.WriteFile(usageFile(), b, 0o644)
}

func ReadUsage() (*UsageSummary, string) {
	b, err := os.ReadFile(usageFile())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, "usage_none"
		}
		return nil, "usage_unread"
	}
	var s UsageSummary
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, "usage_unread"
	}
	s.Source = "file"
	s.AgeSec = (time.Now().UnixMilli() - s.At) / 1000
	if s.AgeSec < 0 {
		s.AgeSec = 0
	}
	return &s, ""
}

func localToday() (string, string) {
	now := time.Now()
	return now.Format("2006-01-02"), now.AddDate(0, 0, -6).Format("2006-01-02")
}

func rollUsageModels(days []UsageModelDay, today, d7from string, localProv map[string]bool) []UsageModelRoll {
	type acc struct {
		today, d7, d30 int64
		cost           float64
		calls          int
	}
	by := map[string]*acc{}
	var order []string
	for _, d := range days {
		if d.Tokens == 0 && d.Count == 0 {
			continue // 0 token 又没调用次数的行没意义
		}
		k := d.Provider + "/" + d.Model
		a := by[k]
		if a == nil {
			a = &acc{}
			by[k] = a
			order = append(order, k)
		}
		a.d30 += d.Tokens
		a.cost += d.Cost
		a.calls += d.Count
		if d.Date >= d7from {
			a.d7 += d.Tokens
		}
		if d.Date == today {
			a.today += d.Tokens
		}
	}
	out := make([]UsageModelRoll, 0, len(order))
	for _, k := range order {
		a := by[k]
		prov, model := k, ""
		if i := strings.Index(k, "/"); i >= 0 {
			prov, model = k[:i], k[i+1:]
		}
		out = append(out, UsageModelRoll{
			Provider: prov, Model: model, Today: a.today, D7: a.d7, D30: a.d30,
			Cost: a.cost, Paid: a.cost > 0, Calls: a.calls, Local: localProv[prov],
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Today != out[j].Today {
			return out[i].Today > out[j].Today
		}
		return out[i].D30 > out[j].D30
	})
	return out
}

func usageCNY(usd float64, rate float64) float64 {
	if rate <= 0 {
		rate = usageRateDefault
	}
	return usd * rate
}

const (
	usageRateDefault = 7.1
	usageRateURL = "https://open.er-api.com/v6/latest/USD"
)

func fetchUsageRate() float64 {
	c := &http.Client{Timeout: 6 * time.Second}
	r, err := c.Get(usageRateURL)
	if err != nil {
		return 0
	}
	defer r.Body.Close()
	if r.StatusCode != 200 {
		return 0
	}
	var j struct {
		Rates map[string]float64 `json:"rates"`
	}
	if err := json.NewDecoder(r.Body).Decode(&j); err != nil {
		return 0
	}
	return j.Rates["CNY"]
}

func usageLocalProviders(p Paths) map[string]bool {
	set := map[string]bool{"openclaw": true}
	c, err := LoadOCConfig(p)
	if err != nil || c == nil {
		return set
	}
	for _, pr := range c.Providers {
		if host, _ := splitURL(pr.BaseURL); host == "local" {
			set[pr.Name] = true
		}
	}
	return set
}

func trimTail(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}

func UsageFileAge() int64 {
	fi, err := os.Stat(usageFile())
	if err != nil {
		return -1
	}
	return int64(time.Since(fi.ModTime()).Seconds())
}
