package fb

//

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

func StopAll(ctx context.Context, p Paths, dry, withGate bool) (*OpResult, error) {
	kill := map[int]string{} // 端口 → 它是谁（档位名 / provider 名），只给 DryRun 与日志看
	keep := map[int]string{}

	rurl := p.RouterURL
	rs := AskRouter(ctx, rurl)
	if rs == nil {
		if dr := DetectRouter(ctx); dr.Value != "" {
			rurl = dr.Value
			rs = AskRouter(ctx, rurl)
		}
	}
	routerPort := portOfURL(rurl)

	gate := 0
	if withGate {
		if routerPort > 0 && PortUp(routerPort) {
			kill[routerPort] = "router"
			gate++
		}
		if p.SwapPort > 0 && PortUp(p.SwapPort) {
			kill[p.SwapPort] = "llama-swap"
			gate++
		}
	}
	if rs != nil {
		for tier, port := range rs.Tiers {
			if port > 0 && port != routerPort && PortUp(port) {
				kill[port] = "tier " + tier
			}
		}
	}

	if oc, err := LoadOCConfig(p); err == nil {
		EnrichKinds(p, oc) // 类型优先读模型文件自己的元数据（有缓存，不重读文件）
		for _, pr := range oc.Providers {
			port := pr.Port()
			if pr.Host() != "local" || port <= 0 || port == routerPort || port == p.SwapPort {
				continue
			}
			small := false
			for _, m := range pr.Models {
				k := m.KindFile
				if k == "" {
					k = m.Kind
				}
				if k == "embed" || k == "vision" {
					small = true
				}
			}
			if !PortUp(port) {
				continue
			}
			if small {
				keep[port] = "provider:" + pr.Name
			} else {
				kill[port] = "provider:" + pr.Name
			}
		}
	}

	unload := "none"
	var sw *Swap
	if s, err := NewSwap(p); err == nil && s.Health(ctx) == nil {
		sw = s
		if ids := s.Running(ctx); len(ids) > 0 {
			sort.Strings(ids)
			unload = strings.Join(ids, ", ")
		} else {
			unload = "nothing loaded"
		}
	}

	ports := sortedPorts(kill)
	kept := sortedPorts(keep)
	if dry {
		return op("stop_all_dry", true).
			subj("ports", joinOrNone(ports), "keep", joinOrNone(kept), "unload", unload, "gate", itoa(gate)), nil
	}
	swapBusy := sw != nil && unload != "none" && unload != "nothing loaded"
	if len(kill) == 0 && !swapBusy {
		return op("stop_all_none", false).subj("keep", joinOrNone(kept)), nil
	}
	var killed, failed []string
	for port := range kill {
		if err := killOnPort(port); err != nil {
			failed = append(failed, fmt.Sprintf("%d: %s", port, err.Error()))
			continue
		}
		killed = append(killed, itoa(port))
	}
	if swapBusy {
		_ = sw.Stop(ctx)
	}
	time.Sleep(2 * time.Second)
	var still []string
	for port := range kill {
		if PortUp(port) {
			still = append(still, itoa(port))
		}
	}
	sort.Strings(killed)
	res := op("stop_all_done", false).
		subj("killed", joinOrNone(killed), "keep", joinOrNone(kept), "still", joinOrNone(still), "unload", unload, "gate", itoa(gate))
	if len(failed) > 0 {
		res.withNote(strings.Join(failed, "; "))
	}
	return res, nil
}

func sortedPorts(m map[int]string) []string {
	var out []string
	for port := range m {
		out = append(out, itoa(port))
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := atoiSafe(out[i]), atoiSafe(out[j])
		return a < b
	})
	return out
}

func atoiSafe(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int(c-'0')
	}
	return n
}
