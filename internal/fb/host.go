package fb

import (
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/mem"
)

type HostInfo struct {
	CPUName  string `json:"cpuName"`
	CPUCores int    `json:"cpuCores"`
	CPULoad  int    `json:"cpuLoad"`  // 整机使用率 %（0-100）；读不到为 -1
	MemTotal uint64 `json:"memTotal"` // 字节
	MemUsed  uint64 `json:"memUsed"`  // 字节（总量减可用）
	MemOK    bool   `json:"memOK"`
	CPUOK    bool   `json:"cpuOK"`
	Err      string `json:"err"`
}

var (
	cpuNameOnce sync.Once
	cpuNameVal  string
	cpuCoresVal int
)

func cleanCPUName(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	if i := strings.Index(s, "Family "); i > 0 {
		return strings.TrimSpace(s[:i])
	}
	return s
}

func cpuIdentity() {
	cpuNameOnce.Do(func() {
		if infos, err := cpu.Info(); err == nil && len(infos) > 0 {
			cpuNameVal = cleanCPUName(infos[0].ModelName)
		}
		if n, err := cpu.Counts(true); err == nil {
			cpuCoresVal = n
		}
		if cpuNameVal == "" {
			cpuNameVal = runtime.GOARCH
		}
	})
}

func Host() HostInfo {
	cpuIdentity()
	h := HostInfo{CPUName: cpuNameVal, CPUCores: cpuCoresVal, CPULoad: -1}
	if per, err := cpu.Percent(250*time.Millisecond, false); err == nil && len(per) > 0 {
		h.CPULoad = int(per[0] + 0.5)
		h.CPUOK = true
	} else if err != nil && h.Err == "" {
		h.Err = "cpu: " + err.Error()
	}
	if vm, err := mem.VirtualMemory(); err == nil {
		h.MemTotal = vm.Total
		h.MemUsed = vm.Used
		h.MemOK = true
	} else if err != nil && h.Err == "" {
		h.Err = "mem: " + err.Error()
	}
	return h
}
