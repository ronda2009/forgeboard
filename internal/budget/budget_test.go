package budget

import (
	"math"
	"strings"
	"testing"
)

//
var meta390 = Meta{
	Name: "coder390-27b", FileMiB: 18711, Layers: 65, KVHeads: 4, KVDim: 256,
	TrainCtx: 262144, FullIntval: 4,
}

var metaVL = Meta{
	Name: "Qwen3-VL-8B", FileMiB: 8305, Layers: 36, KVHeads: 8, KVDim: 128,
	TrainCtx: 262144, HasMMProj: true,
}

func card3090(free int) Card {
	return Card{UUID: "GPU-9fe598c2-7f21-f87b-bed5-a5f0a09b06ac", Name: "RTX 3090", TotalMiB: 24576, FreeMiB: free, Label: "CUDA0", Visible: true}
}

func card3080(free int) Card {
	return Card{UUID: "GPU-6e480ee8-b501-8658-8659-9b33519f0461", Name: "RTX 3080", TotalMiB: 20480, FreeMiB: free, Label: "CUDA1", Visible: true}
}

func TestFullLayersUsesInterval(t *testing.T) {
	if got := meta390.FullLayers(); got != 16 {
		t.Fatalf("65 层 / 间隔 4 应为 16 层，算成 %d", got)
	}
	if got := metaVL.FullLayers(); got != 36 {
		t.Fatalf("没写间隔就该按全部 36 层，算成 %d", got)
	}
}

func TestKVPerTokenBytes(t *testing.T) {
	want := map[KVType]float64{KVF16: 0.0625, KVQ8_0: 0.03515625, KVQ4_0: 0.01953125}
	for kv, w := range want {
		if got := meta390.KVPerTokMiB(kv); math.Abs(got-w) > 1e-9 {
			t.Errorf("390 %s 每 token KV 应为 %v MiB，算成 %v", kv, w, got)
		}
	}
}

func TestNeedMatchesMeasured(t *testing.T) {
	const measured = 21771
	weightsPlusKV := meta390.NeedForMiB(131072, KVQ4_0) - DefaultMarginMiB
	if weightsPlusKV > measured {
		t.Fatalf("公式算的权重+KV %d MiB 已超过实测 %d MiB，公式偏大", weightsPlusKV, measured)
	}
	need := meta390.NeedForMiB(131072, KVQ4_0)
	if need < measured {
		t.Fatalf("余量不够：算出 %d MiB，实测 %d MiB", need, measured)
	}
	if need-measured > 400 {
		t.Errorf("余量偏大（%d MiB 对实测 %d MiB，差 %d），会白拒掉能起的模型", need, measured, need-measured)
	}
	t.Logf("128K+q4_0：权重+KV=%d 实测=%d 公式(含余量)=%d", weightsPlusKV, measured, need)
}

func TestSolveGivesModelItsContext(t *testing.T) {
	p := Solve(meta390, []Card{card3090(23303)})
	if p.Code != "" {
		t.Fatalf("空 3090 上应能起，却判 %s：%s", p.Code, p.Reason)
	}
	if p.Ctx < 131072 {
		t.Errorf("ctx %d 小于 131072，公式偏保守", p.Ctx)
	}
	if p.Ctx > meta390.TrainCtx {
		t.Errorf("ctx %d 超过模型训练上限 %d", p.Ctx, meta390.TrainCtx)
	}
	if p.Ctx%1024 != 0 {
		t.Errorf("ctx %d 不是 1024 的倍数", p.Ctx)
	}
	if p.KV != KVQ4_0 {
		t.Errorf("这张卡只有 q4_0 才塞得下大上下文，应降 q4_0，实际 %s", p.KV)
	}
	for _, want := range []string{"KV per token", "expected", "model header", "ctx "} {
		if !strings.Contains(p.Why, want) {
			t.Errorf("Why 里缺 %q（要能事后核对算的依据）：%s", want, p.Why)
		}
	}
	t.Logf("空 3090：ctx=%d kv=%s 预计 %d MiB", p.Ctx, p.KV, p.NeedMiB)
}

func TestSolveRefusesWhenFull(t *testing.T) {
	busy := card3090(599)
	busy.Holder = "llama-server.exe(coder390-27b)"
	p := Solve(metaVL, []Card{busy})
	if p.Code != "not_enough_vram" {
		t.Fatalf("单卡被占满应拒绝，实际 %s（ctx=%d）", p.Code, p.Ctx)
	}
	for _, want := range []string{"not enough VRAM", "MiB", "held by llama-server.exe(coder390-27b)"} {
		if !strings.Contains(p.Reason, want) {
			t.Errorf("拒绝理由缺 %q：%s", want, p.Reason)
		}
	}
	t.Logf("拒绝理由：%s", p.Reason)
}

func TestSolvePicksTheFreeCard(t *testing.T) {
	cards := []Card{card3090(2805), card3080(12000)}
	cards[0].Holder = "llama-server.exe(coder390-27b)"
	p := Solve(metaVL, cards)
	if p.Code != "" {
		t.Fatalf("3080 有空位就该起，却判 %s：%s", p.Code, p.Reason)
	}
	if p.Card.UUID != card3080(0).UUID {
		t.Errorf("应挑 3080，实际挑了 %s(%s)", p.Card.Name, p.Card.Label)
	}
	if p.NeedMiB > p.Card.FreeMiB {
		t.Errorf("预计 %d MiB 超过所选卡的空闲 %d MiB", p.NeedMiB, p.Card.FreeMiB)
	}
}

func TestSolveSkipsInvisibleCards(t *testing.T) {
	hidden := card3080(16000)
	hidden.Visible = false
	p := Solve(metaVL, []Card{card3090(2805), hidden})
	if p.Code != "not_enough_vram" {
		t.Fatalf("3080 看不见时不该起，实际 %s（挑了 %s）", p.Code, p.Card.Label)
	}
	if !strings.Contains(p.Reason, "CUDA_VISIBLE_DEVICES") {
		t.Errorf("3080 看不见时，拒绝理由要点出旁边那张空卡被挡了：%s", p.Reason)
	}
	only := Solve(metaVL, []Card{hidden})
	if only.Code != "no_visible_card" {
		t.Fatalf("全是看不见的卡应判 no_visible_card，实际 %s", only.Code)
	}
	if !strings.Contains(only.Reason, "CUDA_VISIBLE_DEVICES") {
		t.Errorf("no_visible_card 的理由要点出 CUDA_VISIBLE_DEVICES：%s", only.Reason)
	}
}

func TestSolveWithoutMeta(t *testing.T) {
	p := Solve(Meta{Name: "broken.gguf", FileMiB: 1000}, []Card{card3090(20000)})
	if p.Code != "no_meta" {
		t.Fatalf("缺元数据应判 no_meta，实际 %s", p.Code)
	}
	if !strings.Contains(p.Reason, "broken.gguf") {
		t.Errorf("理由里要带上是哪个模型：%s", p.Reason)
	}
}

func TestFloor1024(t *testing.T) {
	for _, c := range [][2]int{{0, 0}, {1023, 0}, {1024, 1024}, {2500, 2048}, {-5, 0}} {
		if got := floor1024(c[0]); got != c[1] {
			t.Errorf("floor1024(%d) 应为 %d，实际 %d", c[0], c[1], got)
		}
	}
}
