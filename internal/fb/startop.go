package fb

import (
	"context"
	"fmt"
	"strings"
)

func StartOp(ctx context.Context, p Paths, m Model, dry bool) (*OpResult, error) {
	if dry {
		r := op("start_dry", true)
		switch m.Engine {
		case "llama", "llama-swap":
			pl, args := PlanStart(p, ModelGGUF(p, m))
			cmd := SwapCmdOf(p, m.Key)
			if cmd == "" {
				cmd = "llama-swap loads " + m.Key + " (one chat request triggers it)"
			} else if args != "" {
				cmd = setCmdFlags(cmd, args)
			}
			r.subj("cmd", cmd, "why", orDash(pl.Why))
			if pl.Code != "" {
				r.subj("verdict", pl.Code)
				r.withNote(pl.Reason)
			}
		case "strata":
			r.subj("cmd", fmt.Sprintf("GET %s/__switch?tier=%s", p.RouterURL, tierNameOf(m)))
			r.subj("why", "tier switch goes through the router")
		default:
			r.subj("cmd", "engine "+m.Engine)
			r.subj("why", "not supported yet")
		}
		return r, nil
	}

	err := Start(ctx, p, m, false)
	if err == nil {
		pl, _ := PlanStart(p, ModelGGUF(p, m))
		if pl.Ctx <= 0 {
			return op("start_done_bare", false).subj("model", m.Key), nil // 没有上下文/卡这些数据：不把 {ctx} 这类空位原样留在句子里
		}
		return op("start_done", false).subj("model", m.Key).
			subj("ctx", itoa(pl.Ctx), "kv", string(pl.KV), "card", orNone(pl.Card.Name), "need", itoa(pl.NeedMiB)), nil
	}
	pe, ok := err.(*PlanError)
	if !ok {
		return op("start_fail", false).subj("model", m.Key).withNote(strings.TrimSpace(err.Error())), nil
	}
	r := op("start_"+pe.Code, false).subj("model", m.Key)
	for k, v := range pe.Args {
		r.subj(k, v)
	}
	if pe.Raw != "" {
		r.withNote(pe.Raw)
	}
	return r, nil
}

func StopOp(ctx context.Context, p Paths, m Model, dry bool) (*OpResult, error) {
	if dry {
		r := op("stop_dry", true).subj("model", m.Key)
		switch m.Engine {
		case "strata":
			port := m.Port
			if port == 0 {
				if rs := AskRouter(ctx, p.RouterURL); rs != nil {
					port = rs.Tiers[tierNameOf(m)]
				}
			}
			r.subj("cmd", fmt.Sprintf("taskkill the process tree listening on port %d", port))
		default:
			r.subj("cmd", "POST /api/models/unload/"+m.Key)
		}
		return r, nil
	}
	if err := Stop(ctx, p, m, false); err != nil {
		return op("stop_fail", false).subj("model", m.Key).withNote(strings.TrimSpace(err.Error())), nil
	}
	return op("stop_done", false).subj("model", m.Key), nil
}

func ReloadSwapOp(ctx context.Context, p Paths, dry bool) (*OpResult, error) {
	s, err := NewSwap(p)
	if err != nil {
		return nil, err
	}
	hash, stale, err := SwapConfigFingerprint()
	if err != nil {
		return nil, err
	}
	short := hash
	if len(short) > 12 {
		short = short[:12]
	}
	if dry {
		r := op("reload_dry", true).subj("hash", short, "stale", bstr(stale), "running", "—")
		if busy := s.Running(ctx); len(busy) > 0 {
			r.subj("running", strings.Join(busy, ", "))
		}
		return r, nil
	}
	if !stale && s.Health(ctx) == nil {
		return op("reload_fresh", false).subj("hash", short), nil
	}
	if busy := s.Running(ctx); len(busy) > 0 {
		return op("reload_blocked", false).subj("hash", short, "running", strings.Join(busy, ", ")), nil
	}
	if err := s.Restart(ctx, p); err != nil {
		return op("reload_fail", false).subj("hash", short).withNote(err.Error()), nil
	}
	LogAction("reload llama-swap: config " + short + " (restarted, models were unloaded)")
	return op("reload_done", false).subj("hash", short), nil
}

func bstr(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}
