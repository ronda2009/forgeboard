package app

import (
	"context"
	"sync"
	"time"

	"github.com/ronda2009/forgeboard/internal/fb"
)

func (a *App) tasksCached() ([]fb.TaskItem, string) {
	taskMu.Lock()
	defer taskMu.Unlock()
	if !taskBusy && (len(taskCache) == 0 || time.Since(taskAt) > 10*time.Minute) {
		taskBusy = true
		go a.refreshTasks()
	}
	if len(taskCache) == 0 {
		if taskNote == "" {
			return nil, "tasks-loading" // 首轮还在问：界面显示「正在读」而不是报错
		}
		return nil, taskNote
	}
	return taskCache, ""
}

func (a *App) refreshTasks() {
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	rows, err := fb.TaskList(ctx)
	taskMu.Lock()
	if err != nil {
		if len(taskCache) == 0 {
			taskNote = err.Error() // 一次都没成功过：把原因给界面
		}
	} else {
		taskCache = rows
		taskNote = ""
	}
	taskAt = time.Now()
	taskBusy = false
	taskMu.Unlock()
}

var (
	taskMu    sync.Mutex
	taskCache []fb.TaskItem
	taskNote  string
	taskAt    time.Time
	taskBusy  bool
)
