//go:build !windows

package fb

// 非 Windows 没有对应的每进程显存统计：一律回 false，界面退回体积估算。
func procVRAMByPID() (map[int]int, bool) { return nil, false }

func procVRAMCached(force bool) (map[int]int, bool) { return procVRAMByPID() }

func invalidateVRAMCache() {}
