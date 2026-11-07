package app

//

import (
	"context"
	"sync"
	"time"

	"github.com/ronda2009/forgeboard/internal/fb"
)

func (a *App) enrichCronRuns(oc fb.OC, rows []fb.Cron) {
	if !oc.Available() {
		return
	}
	var ids []string
	for _, c := range rows {
		if c.Needs {
			ids = append(ids, c.ID)
		}
	}
	if len(ids) == 0 {
		return
	}
	runMu.Lock()
	cached := map[string]string{}
	for k, v := range runCache {
		cached[k] = v
	}
	stale := time.Since(runAt) > 10*time.Minute
	if !runBusy && (stale || len(cached) == 0) {
		runBusy = true
		go a.refreshRunModels(oc, ids)
	}
	runMu.Unlock()

	for i := range rows {
		if v, ok := cached[rows[i].ID]; ok {
			rows[i].RunModel = v
		}
	}
}

func (a *App) refreshRunModels(oc fb.OC, ids []string) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	m := oc.RunsLatest(ctx, ids)
	runMu.Lock()
	if len(m) > 0 { // 一次全失败（网关在重启之类）就留着旧值，别把好数据清空
		runCache = m
		runAt = time.Now()
	}
	runBusy = false
	runMu.Unlock()
}

var (
	runMu    sync.Mutex
	runCache map[string]string
	runAt    time.Time
	runBusy  bool
)
