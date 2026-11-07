package fb

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/ronda2009/forgeboard/internal/budget"
)

func twoCards() []budget.Card {
	return []budget.Card{
		{UUID: "GPU-a", Name: "RTX 3090", TotalMiB: 24576, FreeMiB: 20000, Label: "CUDA0", Visible: true},
		{UUID: "GPU-b", Name: "RTX 3080", TotalMiB: 20480, FreeMiB: 15000, Label: "CUDA1", Visible: true},
	}
}

func TestGroupsJSONMultiCardIsValidJSON(t *testing.T) {
	out := groupsJSON(twoCards(), []string{"coder390-27b", "qwen3-vl-8b-instruct-unredac"})
	doc := `{"$schema": "x",` + strings.TrimSpace(out) + `"models": {}}`
	var v map[string]any
	if err := json.Unmarshal([]byte(doc), &v); err != nil {
		t.Fatalf("拼出来的配置不是有效 JSON: %v\n%s", err, doc)
	}
	g, ok := v["groups"].(map[string]any)
	if !ok {
		t.Fatalf("两张可见卡应写 groups，结果没有：%s", doc)
	}
	one, ok := g["forgeboard-coexist"].(map[string]any)
	if !ok {
		t.Fatalf("组名不对：%v", g)
	}
	if swap, ok := one["swap"].(bool); !ok || swap {
		t.Errorf("swap 必须是 false（true 就是起新的先卸旧的，本机那次顶掉模型就是这么来的）：%v", one["swap"])
	}
	if ex, ok := one["exclusive"].(bool); !ok || ex {
		t.Errorf("exclusive 必须是 false（true 会把别的组也卸掉）：%v", one["exclusive"])
	}
	mem, _ := one["members"].([]any)
	if len(mem) != 2 {
		t.Errorf("members 应含全部 %d 个模型，实际 %d：%v", 2, len(mem), mem)
	}
}

func TestGroupsJSONSingleCardOmitsGroups(t *testing.T) {
	cards := twoCards()
	cards[1].Visible = false
	if out := groupsJSON(cards, []string{"a", "b"}); strings.Contains(out, "groups") {
		t.Errorf("只有一张可见卡时不该写共存组（这种机器就该一个一个用）：%s", out)
	}
	if out := groupsJSON(twoCards(), nil); strings.Contains(out, "groups") {
		t.Errorf("没有模型时不该写 groups：%s", out)
	}
}

func TestSetCmdFlagsReplacesComputedFlags(t *testing.T) {
	old := `D:\llama-server.exe -m "D:\models\coder 390.gguf" -ngl 99 -c 32768 --host 127.0.0.1 --port ${PORT} --jinja`
	nw := setCmdFlags(old, "-c 131072 -ctk q4_0 -ctv q4_0 --device CUDA0")
	for _, want := range []string{"-c 131072", "-ctk q4_0", "-ctv q4_0", "--device CUDA0", `"D:\models\coder 390.gguf"`, "${PORT}", "--jinja"} {
		if !strings.Contains(nw, want) {
			t.Errorf("盖完缺 %q：%s", want, nw)
		}
	}
	if strings.Contains(nw, "-c 32768") {
		t.Errorf("旧的 -c 32768 没被盖掉：%s", nw)
	}
	if strings.Count(nw, " -c ") != 1 {
		t.Errorf("-c 应当只有一份：%s", nw)
	}
}
