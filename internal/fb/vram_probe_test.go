package fb

import (
	"encoding/binary"
	"os"
	"testing"
)

// 造一份和系统统计同格式的缓冲区：两条记录 + 名字区。
func TestParseVRAMItems(t *testing.T) {
	names := []string{"pid_6256_luid_0x00000000_0x000723AF_phys_0", "pid_9512_luid_0x00000000_0x00074FA3_phys_0"}
	fixed := len(names) * pdhItemStride
	size := fixed
	for _, n := range names {
		size += len(n) + 1
	}
	buf := make([]byte, size)
	vals := []int64{23805 << 20, 0}
	at := fixed
	for i := range names {
		off := i * pdhItemStride
		binary.LittleEndian.PutUint32(buf[off+pdhStatusAt:], 0)
		binary.LittleEndian.PutUint64(buf[off+pdhValueAt:], uint64(vals[i]))
		copy(buf[at:], names[i])
		at += len(names[i]) + 1
	}
	got := parseVRAMItems(buf, len(names))
	if len(got) != 2 || got[6256] != 23805 || got[9512] != 0 {
		t.Fatalf("got %v, want {6256:23805, 9512:0}", got)
	}
	if _, ok := pidFromInstance("luid_bogus"); ok {
		t.Fatal("luid_bogus should not parse")
	}
	if pid, ok := pidFromInstance("pid_9512_luid_x"); !ok || pid != 9512 {
		t.Fatalf("pidFromInstance=%d,%v", pid, ok)
	}
}

// 真机探针：确认「按进程分显存」真取到数（设 FORGEBOARD_LIVE=1 才跑，免得没显卡的环境红）。
func TestLiveVRAMProbe(t *testing.T) {
	if os.Getenv("FORGEBOARD_LIVE") == "" {
		t.Skip("FORGEBOARD_LIVE 未设置")
	}
	v, ok := procVRAMByPID()
	if !ok {
		t.Fatal("取不到每进程显存")
	}
	big := 0
	for pid, mib := range v {
		if mib > 200 {
			t.Logf("pid=%d %d MiB", pid, mib)
			big++
		}
	}
	if big == 0 {
		t.Fatal("没有任何进程报显存，探针不可用")
	}
}
