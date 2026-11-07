package app

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/ronda2009/forgeboard/internal/fb"
	wailsrt "github.com/wailsapp/wails/v2/pkg/runtime"
)

const usageRefreshEvery = 15 * time.Minute

func (a *App) usageCached() *fb.UsageSummary {
	s, note := fb.ReadUsage()
	if s == nil {
		a.kickUsage(true)
		return &fb.UsageSummary{Note: note}
	}
	if s.AgeSec > int64(usageRefreshEvery.Seconds()) {
		a.kickUsage(false)
	}
	return s
}

func (a *App) kickUsage(force bool) {
	usageMu.Lock()
	if usageBusy {
		usageMu.Unlock()
		return
	}
	age := fb.UsageFileAge()
	if !force && age >= 0 && age < int64(usageRefreshEvery.Seconds()) {
		usageMu.Unlock()
		return
	}
	usageBusy = true
	usageMu.Unlock()
	go a.refreshUsage()
}

func (a *App) refreshUsage() {
	defer func() {
		usageMu.Lock()
		usageBusy = false
		usageMu.Unlock()
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	s, err := fb.FetchUsage(ctx, fb.LoadPaths())
	if err != nil {
		a.push("[usage] refresh failed: " + err.Error())
		return
	}
	if err := fb.WriteUsage(s); err != nil {
		a.push("[usage] kept previous file: " + err.Error())
		a.usageDone()
		return
	}
	fb.LogAction(fmt.Sprintf("[usage] collected windows=%d tokens=%d cache=%s", s.Windows, s.Totals.Tokens, s.Cache))
	a.usageDone()
}

func (a *App) UsageNow() *fb.UsageSummary {
	a.kickUsage(true)
	s, note := fb.ReadUsage()
	if s == nil {
		return &fb.UsageSummary{Note: note}
	}
	return s
}

func (a *App) usageDone() {
	if a.ctx != nil {
		wailsrt.EventsEmit(a.ctx, "usage", "done")
	}
}

func (a *App) kickUsageFirstRound() {
	if fb.UsageFileAge() >= 0 && fb.UsageFileAge() < int64(usageRefreshEvery.Seconds()) {
		return
	}
	a.kickUsage(true)
}

var (
	usageMu   sync.Mutex
	usageBusy bool
)
