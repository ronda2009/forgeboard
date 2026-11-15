//go:build windows

package fb

import (
	"encoding/binary"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// 每个进程实际占用的显存，来自系统自己的 GPU 统计（任务管理器同一个来源）。
// 有了它，「哪个模型占了多少显存」就能按进程分开量，不用拿整张卡的占用去猜。
var (
	pdhDLL                   = windows.NewLazySystemDLL("pdh.dll")
	procPdhOpenQuery         = pdhDLL.NewProc("PdhOpenQuery")
	procPdhCloseQuery        = pdhDLL.NewProc("PdhCloseQuery")
	procPdhAddEnglishCounter = pdhDLL.NewProc("PdhAddEnglishCounterA")
	procPdhCollectQueryData  = pdhDLL.NewProc("PdhCollectQueryData")
	procPdhGetFormattedArr   = pdhDLL.NewProc("PdhGetFormattedCounterArrayA")
)

const (
	pdhFmtLarge    = 0x00000400
	pdhFmtNoCap100 = 0x00008000
	pdhMoreData    = 0x80000209
	pdhItemStride  = 24 // 一条记录：名字指针(8) 状态(4) 保留(4) 数值(8)
	pdhValueAt     = 16
	pdhStatusAt    = 8
)

var (
	vramCacheMu sync.Mutex
	vramCacheAt time.Time
	vramCache   map[int]int
)

// 界面每 15 秒刷一次，每次都开一轮系统统计要等一秒多，所以留 60 秒缓存。
// 只有起停模型会让它变，而那两种情都走 invalidateVRAMCache。
func procVRAMCached(force bool) (map[int]int, bool) {
	vramCacheMu.Lock()
	defer vramCacheMu.Unlock()
	if !force && vramCache != nil && time.Since(vramCacheAt) < 60*time.Second {
		return vramCache, true
	}
	m, ok := procVRAMByPID()
	if ok {
		vramCache, vramCacheAt = m, time.Now()
	}
	return m, ok
}

func invalidateVRAMCache() {
	vramCacheMu.Lock()
	vramCache = nil
	vramCacheMu.Unlock()
}

// procVRAMByPID 返回 进程号 -> 显存占用 MiB（多张卡相加）。取不到返回 false。
func procVRAMByPID() (map[int]int, bool) {
	var q uintptr
	if r, _, _ := procPdhOpenQuery.Call(0, 0, uintptr(unsafe.Pointer(&q))); r != 0 {
		return nil, false
	}
	defer procPdhCloseQuery.Call(q)

	var c uintptr
	path, err := syscall.BytePtrFromString(`\GPU Process Memory(*)\Dedicated Usage`)
	if err != nil {
		return nil, false
	}
	if r, _, _ := procPdhAddEnglishCounter.Call(q, uintptr(unsafe.Pointer(path)), 0, uintptr(unsafe.Pointer(&c))); r != 0 {
		return nil, false
	}
	// 第一轮只登记实例，隔一会儿第二轮才有数
	if r, _, _ := procPdhCollectQueryData.Call(q); r != 0 {
		return nil, false
	}
	format := uintptr(pdhFmtLarge | pdhFmtNoCap100)
	deadline := time.Now().Add(6 * time.Second)
	for {
		time.Sleep(600 * time.Millisecond)
		if r, _, _ := procPdhCollectQueryData.Call(q); r != 0 {
			return nil, false
		}
		var bufSize, itemCount uint32
		procPdhGetFormattedArr.Call(c, format, uintptr(unsafe.Pointer(&bufSize)), uintptr(unsafe.Pointer(&itemCount)), 0)
		if bufSize >= uint32(itemCount)*pdhItemStride && itemCount > 0 {
			buf := make([]byte, bufSize)
			if r, _, _ := procPdhGetFormattedArr.Call(c, format, uintptr(unsafe.Pointer(&bufSize)), uintptr(unsafe.Pointer(&itemCount)), uintptr(unsafe.Pointer(&buf[0]))); r == 0 {
				if out := parseVRAMItems(buf, int(itemCount)); len(out) > 0 {
					return out, true
				}
			}
		}
		if time.Now().After(deadline) {
			return nil, false
		}
	}
}

// 缓冲区 = itemCount 条定长记录 + 紧随其后的名字区（按同一顺序、以 \0 分隔）。
func parseVRAMItems(buf []byte, itemCount int) map[int]int {
	fixed := itemCount * pdhItemStride
	if len(buf) < fixed {
		return nil
	}
	names := strings.Split(string(buf[fixed:]), "\x00")
	if len(names) < itemCount {
		return nil
	}
	out := map[int]int{}
	for i := 0; i < itemCount; i++ {
		off := i * pdhItemStride
		if binary.LittleEndian.Uint32(buf[off+pdhStatusAt:]) != 0 {
			continue // 这条这一轮没数（进程刚退）
		}
		value := int64(binary.LittleEndian.Uint64(buf[off+pdhValueAt:]))
		pid, ok := pidFromInstance(names[i])
		if !ok {
			continue
		}
		if value < 0 {
			value = 0 // 纯 CPU 跑的进程：占 0，也要记下来（说明它真没吃显存）
		}
		out[pid] += int(value / (1024 * 1024))
	}
	return out
}

// 实例名形如 pid_6256_luid_0x00000000_0x000723AF_phys_0
func pidFromInstance(name string) (int, bool) {
	if len(name) < 5 || name[:4] != "pid_" {
		return 0, false
	}
	rest := name[4:]
	n := 0
	for n < len(rest) && rest[n] >= '0' && rest[n] <= '9' {
		n++
	}
	if n == 0 {
		return 0, false
	}
	pid := 0
	for _, ch := range rest[:n] {
		pid = pid*10 + int(ch-'0')
		if pid > 1<<30 {
			return 0, false
		}
	}
	return pid, pid > 0
}
