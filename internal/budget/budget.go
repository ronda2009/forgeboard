package budget

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

type KVType string

const (
	KVF16  KVType = "f16"
	KVQ8_0 KVType = "q8_0"
	KVQ4_0 KVType = "q4_0"
)

var kvBytesPerElem = map[KVType]float64{
	KVF16:  2.0,
	KVQ8_0: 1.125,
	KVQ4_0: 0.625,
}

var KVOrder = []KVType{KVF16, KVQ8_0, KVQ4_0}

const MinUsefulCtx = 8192

const DefaultMarginMiB = 768

type Meta struct {
	Name       string
	FileMiB    int
	Layers     int
	KVHeads    int
	KVDim      int // attention.key_length（与 value_length 不等时取大的那个）
	TrainCtx   int // <arch>.context_length
	FullIntval int // <arch>.full_attention_interval，0 = 每层都是全注意力
	HasMMProj  bool
}

func (m Meta) Ok() bool {
	return m.FileMiB > 0 && m.Layers > 0 && m.KVHeads > 0 && m.KVDim > 0 && m.TrainCtx > 0
}

type Card struct {
	UUID     string
	Index    int // nvidia-smi physical index
	Name     string
	TotalMiB int
	FreeMiB  int
	Label    string
	Visible  bool
	Holder   string // 现在谁占着这张卡（进程名），只用来写拒绝理由
}

type Plan struct {
	Ctx        int
	KV         KVType
	NeedMiB    int
	KVPerTokKB int
	Card       Card
	Why        string            // 为什么是这个 ctx / 为什么降了 KV（进日志，一行一台）
	Code       string            // "" = 可行 | no_meta | not_enough_vram | no_visible_card
	Reason     string            // 起不来时的人话理由（日志/CLI 用）
	Args       map[string]string // 给界面的数字与名字（need/free/holder/card/ctx/kv…）
}

func (m Meta) FullLayers() int {
	if m.FullIntval <= 1 {
		return m.Layers
	}
	n := m.Layers / m.FullIntval
	if n < 1 {
		return 1
	}
	return n
}

func (m Meta) KVPerTokMiB(kv KVType) float64 {
	b, ok := kvBytesPerElem[kv]
	if !ok {
		b = 2.0
	}
	perTok := 2.0 * float64(m.FullLayers()) * float64(m.KVHeads) * float64(m.KVDim) * b
	return perTok / (1024 * 1024)
}

func (m Meta) NeedForMiB(ctx int, kv KVType) int {
	return m.FileMiB + int(math.Ceil(m.KVPerTokMiB(kv)*float64(ctx))) + DefaultMarginMiB + m.MMProjMiB()
}

func (m Meta) MMProjMiB() int {
	if !m.HasMMProj {
		return 0
	}
	return 1024
}

func floor1024(n int) int {
	if n < 0 {
		return 0
	}
	return n / 1024 * 1024
}

func (m Meta) MaxCtx(c Card, kv KVType) int {
	room := c.FreeMiB - m.FileMiB - DefaultMarginMiB - m.MMProjMiB()
	if room <= 0 {
		return 0
	}
	per := m.KVPerTokMiB(kv)
	if per <= 0 {
		return m.TrainCtx
	}
	return floor1024(int(math.Floor(float64(room) / per)))
}

func Solve(m Meta, cards []Card) Plan {
	if !m.Ok() {
		return Plan{
			Code:   "no_meta",
			Reason: fmt.Sprintf("cannot read this model's own header (file size / layers / kv heads / head dim / train ctx): keeping the parameters from the config file (model=%s)", orNone(m.Name)),
			Args:   map[string]string{"model": orNone(m.Name)},
		}
	}
	vis := make([]Card, 0, len(cards))
	for _, c := range cards {
		if c.Visible && c.FreeMiB > 0 {
			vis = append(vis, c)
		}
	}
	if len(vis) == 0 {
		return Plan{
			Code:   "no_visible_card",
			Reason: "the inference engine cannot see any GPU (CUDA_VISIBLE_DEVICES is probably hiding them): clear that variable first",
			Args:   map[string]string{"model": orNone(m.Name), "hidden": hiddenList(cards)},
		}
	}
	sort.SliceStable(vis, func(i, j int) bool { return vis[i].FreeMiB > vis[j].FreeMiB })

	best, bestKV, bestCard, found := 0, KVOrder[len(KVOrder)-1], vis[0], false
	for _, c := range vis { // 已按空闲从大到小排：后面的只在 ctx 真更大时才翻盘
		for _, kv := range KVOrder {
			ctx := m.MaxCtx(c, kv)
			if ctx > m.TrainCtx {
				ctx = m.TrainCtx
			}
			if ctx < MinUsefulCtx {
				continue
			}
			if !found || ctx > best {
				best, bestKV, bestCard, found = ctx, kv, c, true
			}
		}
	}
	if found {
		need := m.NeedForMiB(best, bestKV)
		return Plan{
			Ctx:        best,
			KV:         bestKV,
			NeedMiB:    need,
			KVPerTokKB: int(math.Ceil(m.KVPerTokMiB(bestKV) * 1024)),
			Card:       bestCard,
			Why:        m.why(best, bestKV, bestCard),
			Args:       planArgs(m, best, bestKV, need, bestCard),
		}
	}

	kv := KVOrder[len(KVOrder)-1]
	c := vis[0]
	want := m.NeedForMiB(MinUsefulCtx, kv)
	reason := fmt.Sprintf(
		"not enough VRAM: needs at least %d MiB (model %d + KV for %dK context + margin), biggest free card %d MiB (%s %s)%s%s",
		want, m.FileMiB, MinUsefulCtx/1024, c.FreeMiB, orNone(c.Name), orNone(c.Label), heldBy(c),
		hiddenHint(cards, c))
	args := planArgs(m, MinUsefulCtx, kv, want, c)
	args["hidden"] = hiddenList(cards)
	return Plan{
		Code:       "not_enough_vram",
		KV:         kv,
		Ctx:        0,
		NeedMiB:    want,
		Card:       c,
		KVPerTokKB: int(math.Ceil(m.KVPerTokMiB(kv) * 1024)),
		Reason:     reason,
		Args:       args,
	}
}

func planArgs(m Meta, ctx int, kv KVType, need int, c Card) map[string]string {
	return map[string]string{
		"model":    orNone(m.Name),
		"ctx":      strconv.Itoa(ctx),
		"kv":       string(kv),
		"need":     strconv.Itoa(need),
		"file":     strconv.Itoa(m.FileMiB),
		"pertokkb": strconv.Itoa(int(math.Ceil(m.KVPerTokMiB(kv) * 1024))),
		"free":     strconv.Itoa(c.FreeMiB),
		"card":     orNone(c.Name),
		"device":   orNone(c.Label),
		"holder":   orNone(c.Holder),
		"train":    strconv.Itoa(m.TrainCtx),
		"layers":   strconv.Itoa(m.FullLayers()) + "/" + strconv.Itoa(m.Layers),
	}
}

func hiddenList(cards []Card) string {
	var names []string
	for _, c := range cards {
		if c.Visible {
			continue
		}
		names = append(names, fmt.Sprintf("%s %d MiB", orNone(c.Name), c.FreeMiB))
	}
	if len(names) == 0 {
		return ""
	}
	return strings.Join(names, ", ")
}

func hiddenHint(cards []Card, bestVis Card) string {
	best := Card{}
	for _, c := range cards {
		if c.Visible || c.FreeMiB <= bestVis.FreeMiB {
			continue
		}
		if c.FreeMiB > best.FreeMiB {
			best = c
		}
	}
	if best.UUID == "" {
		return ""
	}
	return fmt.Sprintf("; there is also %s with %d MiB free, but the engine cannot see it (CUDA_VISIBLE_DEVICES does not allow it)", orNone(best.Name), best.FreeMiB)
}

func (m Meta) why(ctx int, kv KVType, c Card) string {
	parts := []string{
		fmt.Sprintf("model header: train ctx %d, full-attention layers %d/%d, kv heads %d, head dim %d", m.TrainCtx, m.FullLayers(), m.Layers, m.KVHeads, m.KVDim),
		fmt.Sprintf("KV per token %d KB (%s)", int(math.Ceil(m.KVPerTokMiB(kv)*1024)), kv),
		fmt.Sprintf("ctx %d (model limit %d, this card fits at most %d)", ctx, m.TrainCtx, floor1024(m.MaxCtx(c, kv))),
		fmt.Sprintf("expected %d MiB on %s which has %d MiB free", m.NeedForMiB(ctx, kv), orNone(c.Name), c.FreeMiB),
	}
	if kv != KVF16 {
		parts = append(parts, fmt.Sprintf("KV downgraded to %s: f16 KV does not fit on this card", kv))
	}
	if m.FullIntval > 1 {
		parts = append(parts, fmt.Sprintf("hybrid attention (one full-attention layer every %d): KV counted on full-attention layers only", m.FullIntval))
	}
	return strings.Join(parts, "; ")
}

func NominalCtx(m Meta, p Plan) int {
	if p.Ctx > 0 {
		return p.Ctx
	}
	if m.TrainCtx > 0 {
		return m.TrainCtx
	}
	return 32768
}

func heldBy(c Card) string {
	if c.Holder == "" {
		return ""
	}
	return fmt.Sprintf(", held by %s", c.Holder)
}

func orNone(s string) string {
	if s == "" {
		return "(unknown)"
	}
	return s
}
