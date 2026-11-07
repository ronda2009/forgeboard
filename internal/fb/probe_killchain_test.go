package fb

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"testing"
	"time"
)

func TestProbeKillChain(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("这条链用 taskkill，只在 Windows 上验")
	}
	port := 0
	for p := 8399; p < 8440; p++ {
		if !PortUp(p) {
			port = p
			break
		}
	}
	if port == 0 {
		t.Skip("找不到空端口")
	}

	script := fmt.Sprintf("$l=[Net.Sockets.TcpListener]::new([Net.IPAddress]::Loopback,%d); $l.Start(); Start-Sleep -Seconds 120", port)
	cmd := exec.Command("pwsh", "-NoProfile", "-NonInteractive", "-Command", script)
	cmd.SysProcAttr = hideCmd
	if err := cmd.Start(); err != nil {
		t.Fatalf("start failed: %v", err)
	}
	defer func() {
		_ = cmd.Process.Kill()
	}()

	up := false
	for i := 0; i < 40; i++ {
		if PortUp(port) {
			up = true
			break
		}
		time.Sleep(150 * time.Millisecond)
	}
	t.Logf("port=%d up-before=%v", port, up)
	if !up {
		t.Fatalf("自己起的监听没起来，测不了")
	}

	t0 := time.Now()
	if err := killOnPort(port); err != nil {
		t.Fatalf("killOnPort failed: %v", err)
	}
	waitPortDown(port)
	t.Logf("kill+waitPortDown elapsed=%s up-after=%v", time.Since(t0).Round(time.Millisecond), PortUp(port))
	if PortUp(port) {
		t.Fatalf("杀完端口还在监听 —— 界面刷新就会还显示「在跑」")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	res := PortsUp(ctx, []int{port, 8398})
	t.Logf("PortsUp(%d,%d) = %v", port, 8398, res)
	if res[port] || res[8398] {
		t.Fatalf("PortsUp 把已停的端口算成在跑")
	}
	_ = cmd.Wait()
}
