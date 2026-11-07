package app

import (
	"context"
	"testing"
	"time"
)

func TestProbeMountedRunning(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	a := New(Deps{Version: "probe"})
	a.ctx = ctx
	s, err := a.Snapshot()
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	t.Logf("router current=%s err=%q tiers=%v", s.Router.Current, s.Router.Err, s.Router.Tiers)
	t.Logf("mounted rows=%d", len(s.Mounted))
	for _, m := range s.Mounted {
		if m.Host != "local" {
			continue
		}
		t.Logf("name=%-26s id=%-26s port=%-6d running=%-5v fit=%-8s actKey=%-14s engine=%s",
			m.Name, m.ID, m.Port, m.Running, m.Fit, m.ActKey, m.Engine)
	}
	for _, m := range s.Models {
		if m.Engine == "strata" {
			t.Logf("SCAN-ROW key=%-10s running=%-5v fit=%-8s port=%d", m.Key, m.Running, m.Fit, m.Port)
		}
	}
}
