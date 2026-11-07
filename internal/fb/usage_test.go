package fb

import (
	"context"
	"os"
	"testing"
	"time"
)

const usageFixture = `{
  "startDate": "2026-10-07",
  "endDate": "2026-11-05",
  "cacheStatus": {"status": "fresh", "cachedFiles": 211, "pendingFiles": 0},
  "totals": {"input": 631799332, "output": 82816457, "cacheRead": 8361933495, "totalTokens": 9076549284,
             "totalCost": 42.37, "missingCostEntries": 8747},
  "sessions": [
    {"key": "agent:main:feishu:group:oc_1", "label": "按钮开发组A", "modelProvider": "strata",
     "model": "qwen3.8-flash-next-iq3x", "updatedAt": 1793863152667,
     "usage": {"input": 47028773, "output": 8944516, "cacheRead": 810439353, "totalTokens": 866412642,
               "totalCost": 0, "missingCostEntries": 702, "lastActivity": 1793863487676,
               "messageCounts": {"total": 7457, "toolCalls": 7668}}},
    {"key": "agent:main:cron:b9acb2f0-6717-43f0-89e5-fbe05b243338:trigger", "modelProvider": "scnet", "model": "DeepSeek-V4.1-Flash",
     "updatedAt": 1793800000000,
     "usage": {"input": 100, "output": 200, "cacheRead": 0, "totalTokens": 300, "totalCost": 0.5,
               "missingCostEntries": 0, "messageCounts": {"total": 3, "toolCalls": 2}}}
  ],
  "aggregates": {
    "messages": {"total": 72500, "toolCalls": 74544, "errors": 1920},
    "daily": [
      {"date": "2026-11-04", "tokens": 100131269, "cost": 0.5, "messages": 560, "toolCalls": 603, "errors": 30},
      {"date": "2026-11-05", "tokens": 114000756, "cost": 0.018, "messages": 1013, "toolCalls": 983, "errors": 16}
    ],
    "byModel": [
      {"provider": "strata", "model": "qwen3.8-flash-next-iq3x", "count": 5529,
       "totalTokens": 652942845, "totalCost": 0, "missingCostEntries": 5529}
    ],
    "byProvider": [
      {"provider": "deepseek", "count": 8044, "totalTokens": 2027200478, "totalCost": 42.37, "missingCostEntries": 1179}
    ],
    "modelDaily": [
      {"date": "2026-11-05", "provider": "strata", "model": "qwen3.8-flash-next-iq3x", "tokens": 114000756, "cost": 0, "count": 900},
      {"date": "2026-11-05", "provider": "deepseek", "model": "deepseek-v4-flash", "tokens": 1500000, "cost": 0.018, "count": 13},
      {"date": "2026-11-03", "provider": "strata", "model": "qwen3.8-flash-next-iq3x", "tokens": 50000000, "cost": 0, "count": 400},
      {"date": "2026-11-01", "provider": "deepseek", "model": "deepseek-v4-flash", "tokens": 20000000, "cost": 0.5, "count": 120},
      {"date": "2026-10-20", "provider": "scnet", "model": "DeepSeek-V4.1-Flash", "tokens": 30000000, "cost": 1.25, "count": 800},
      {"date": "2026-10-20", "provider": "openclaw", "model": "delivery-mirror", "tokens": 0, "cost": 0, "count": 0}
    ]
  }
}`

var usageTestLocal = map[string]bool{"strata": true, "llamacpp": true, "ollama": true, "openclaw": true}

func TestUsageParseKeepsEveryNumber(t *testing.T) {
	s, err := parseUsage([]byte(usageFixture), usageTestLocal, 7.0)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if s.Start != "2026-10-07" || s.End != "2026-11-05" {
		t.Errorf("range = %s..%s", s.Start, s.End)
	}
	if s.Cache != "fresh" {
		t.Errorf("cache = %q", s.Cache)
	}
	if s.Totals.Tokens != 9076549284 || s.Totals.Cost != 42.37 || s.Totals.Unpriced != 8747 {
		t.Errorf("totals = %+v", s.Totals)
	}
	if s.Messages != 72500 || s.ToolCalls != 74544 || s.Errors != 1920 {
		t.Errorf("counts = %d/%d/%d", s.Messages, s.ToolCalls, s.Errors)
	}
	if s.Windows != 2 {
		t.Errorf("windows = %d", s.Windows)
	}
	if s.Rate != 7.0 {
		t.Errorf("rate = %v", s.Rate)
	}
	if len(s.Daily) != 2 || s.Daily[1].Tokens != 114000756 {
		t.Errorf("daily = %+v", s.Daily)
	}
	if len(s.ByModel) != 1 || s.ByModel[0].Tokens != 652942845 || s.ByModel[0].Unpriced != 5529 {
		t.Errorf("byModel = %+v", s.ByModel)
	}
	if len(s.ByService) != 1 || s.ByService[0].Cost != 42.37 {
		t.Errorf("byService = %+v", s.ByService)
	}
	today, d7from := localToday()
	if today != "2026-11-05" {
		t.Skipf("fixture days are pinned to 2026-11-05, machine today = %s", today)
	}
	if s.Today.Tokens != 114000756 || s.Today.Cost != 0.018 {
		t.Errorf("today = %+v (want 114000756 / 0.018)", s.Today)
	}
	if s.D7.Tokens != 214132025 {
		t.Errorf("d7 tokens = %d (want 214132025)", s.D7.Tokens)
	}
	if len(s.Models) != 3 {
		t.Fatalf("models = %+v (want 3 rows, the zero-token row dropped)", s.Models)
	}
	m0 := s.Models[0]
	if m0.Model != "qwen3.8-flash-next-iq3x" || m0.Today != 114000756 || m0.D7 != 164000756 || m0.D30 != 164000756 {
		t.Errorf("model row 0 = %+v", m0)
	}
	if m0.Paid {
		t.Errorf("local model should not be marked paid: %+v", m0)
	}
	if !m0.Local {
		t.Errorf("strata is a local provider, row should be marked local: %+v", m0)
	}
	m1 := s.Models[1]
	if m1.Model != "deepseek-v4-flash" || m1.Today != 1500000 || m1.D7 != 21500000 || m1.D30 != 21500000 || !m1.Paid {
		t.Errorf("model row 1 = %+v", m1)
	}
	if m1.Local {
		t.Errorf("deepseek is remote, must not be marked local: %+v", m1)
	}
	m2 := s.Models[2]
	if m2.Today != 0 || m2.D7 != 0 || m2.D30 != 30000000 || m2.Cost != 1.25 {
		t.Errorf("model row 2 (only older days) = %+v", m2)
	}
	_ = d7from
}

func TestUsageFileRoundTrip(t *testing.T) {
	s, err := parseUsage([]byte(usageFixture), usageTestLocal, 7.0)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	s.At = time.Now().Add(-3 * time.Minute).UnixMilli()
	if err := WriteUsage(s); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, note := ReadUsage()
	if note != "" {
		t.Fatalf("read note = %q", note)
	}
	if got.Totals.Tokens != s.Totals.Tokens || len(got.Models) != len(s.Models) {
		t.Errorf("round trip lost data: totals=%d models=%d", got.Totals.Tokens, len(got.Models))
	}
	if got.Source != "file" {
		t.Errorf("source = %q", got.Source)
	}
	if got.AgeSec < 170 || got.AgeSec > 200 {
		t.Errorf("ageSec = %d (want ~180)", got.AgeSec)
	}
	if age := UsageFileAge(); age < 0 {
		t.Errorf("file age = %d", age)
	}
	b, err := os.ReadFile(usageFile())
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if len(b) > 200*1024 {
		t.Errorf("usage file = %d bytes (want small enough to read every refresh)", len(b))
	}
}

func TestUsageEmptyNotWritten(t *testing.T) {
	good, err := parseUsage([]byte(usageFixture), usageTestLocal, 7.0)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if err := WriteUsage(good); err != nil {
		t.Fatalf("write good: %v", err)
	}
	half := *good
	half.Totals.Tokens = 0
	half.Totals.Cost = 0
	if err := WriteUsage(&half); err == nil {
		t.Fatal("empty half-result was accepted - it would blank the panel")
	}
	got, note := ReadUsage()
	if note != "" {
		t.Fatalf("read note = %q", note)
	}
	if got.Totals.Tokens != good.Totals.Tokens {
		t.Errorf("disk copy got clobbered: %d want %d", got.Totals.Tokens, good.Totals.Tokens)
	}
	zero := *good
	zero.Windows = 0
	zero.Totals.Tokens = 0
	if err := WriteUsage(&zero); err != nil {
		t.Errorf("genuinely empty machine should still write: %v", err)
	}
}

func TestUsageDayCutParams(t *testing.T) {
	got := usageParams()
	if got["mode"] != "specific" {
		t.Errorf(`mode = %v (want "specific", else the gateway cuts days in UTC)`, got["mode"])
	}
	if got["utcOffset"] != "UTC+8:00" {
		t.Errorf("utcOffset = %v (want UTC+8:00 on this box)", got["utcOffset"])
	}
	if got["limit"] != usageLimit {
		t.Errorf("limit = %v (want %d, else the gateway caps at 50 windows)", got["limit"], usageLimit)
	}
}

func TestUsageModelRoll(t *testing.T) {
	days := []UsageModelDay{
		{Date: "2026-11-05", Provider: "strata", Model: "a", Tokens: 100, Cost: 0, Count: 2},
		{Date: "2026-11-01", Provider: "strata", Model: "a", Tokens: 50, Cost: 0, Count: 1},
		{Date: "2026-10-28", Provider: "strata", Model: "a", Tokens: 70, Cost: 0, Count: 1},
		{Date: "2026-11-05", Provider: "deepseek", Model: "b", Tokens: 10, Cost: 0.5, Count: 3},
		{Date: "2026-10-28", Provider: "scnet", Model: "c", Tokens: 900, Cost: 2, Count: 9},
		{Date: "2026-11-05", Provider: "openclaw", Model: "d", Tokens: 0, Cost: 0, Count: 0},
	}
	got := rollUsageModels(days, "2026-11-05", "2026-10-30", map[string]bool{"strata": true})
	if len(got) != 3 {
		t.Fatalf("rows = %+v (want 3, the zero row dropped)", got)
	}
	if got[0].Model != "a" || got[0].Today != 100 || got[0].D7 != 150 || got[0].D30 != 220 {
		t.Errorf("row 0 = %+v", got[0])
	}
	if got[1].Model != "b" || got[1].Today != 10 || !got[1].Paid || got[1].Cost != 0.5 {
		t.Errorf("row 1 = %+v", got[1])
	}
	if got[2].Model != "c" || got[2].Today != 0 || got[2].D7 != 0 || got[2].D30 != 900 {
		t.Errorf("row 2 = %+v", got[2])
	}
	if !got[0].Local || got[1].Local || got[2].Local {
		t.Errorf("local flags = %v/%v/%v (want only strata true)", got[0].Local, got[1].Local, got[2].Local)
	}
	if got[0].Calls != 4 || got[2].Calls != 9 {
		t.Errorf("calls = %d / %d", got[0].Calls, got[2].Calls)
	}
}

func TestUsageCNY(t *testing.T) {
	if got := usageCNY(10, 7.2); got != 72 {
		t.Errorf("10 USD @7.2 = %v", got)
	}
	if got := usageCNY(10, 0); got != 10*usageRateDefault {
		t.Errorf("rate 0 should fall back to default: %v", got)
	}
	if usageCNY(0.004, 7.2) <= 0 {
		t.Error("tiny cost rounded to zero")
	}
}

func TestUsageParseRejectsGarbage(t *testing.T) {
	if _, err := parseUsage([]byte("not json"), nil, 0); err == nil {
		t.Fatal("garbage parsed without error")
	}
}

func TestUsageLive(t *testing.T) {
	if os.Getenv("FORGEBOARD_LIVE") != "1" {
		t.Skip("set FORGEBOARD_LIVE=1 to hit the gateway")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	s, err := FetchUsage(ctx, LoadPaths())
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if s.Windows == 0 || s.Totals.Tokens == 0 {
		t.Fatalf("empty result: %+v", s.Totals)
	}
	var sum int64
	for _, m := range s.Models {
		sum += m.D30
	}
	if sum != s.Totals.Tokens {
		t.Errorf("byModel sum %d != totals %d", sum, s.Totals.Tokens)
	}
	t.Logf("windows=%d models=%d tokens=%d cache=%s range=%s..%s", s.Windows, len(s.Models), s.Totals.Tokens, s.Cache, s.Start, s.End)
}
