package fb

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func runPwsh(t *testing.T, label, script string, hide bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	t0 := time.Now()
	cmd := exec.CommandContext(ctx, "pwsh", "-NoProfile", "-NonInteractive", "-Command", "-")
	if hide {
		cmd.SysProcAttr = hideCmd
	}
	cmd.Stdin = strings.NewReader(script)
	var so, se strings.Builder
	cmd.Stdout = &so
	cmd.Stderr = &se
	err := cmd.Run()
	t.Logf("%s hide=%v elapsed=%s err=%v out=%d bytes head=%q err-out=%q",
		label, hide, time.Since(t0).Round(time.Millisecond), err, so.Len(), trunc(so.String(), 120), trunc(se.String(), 200))
}

func TestProbePwsh(t *testing.T) {
	runPwsh(t, "hello", "Write-Output 'hi-from-pwsh'\n", false)
	runPwsh(t, "hello", "Write-Output 'hi-from-pwsh'\n", true)
	runPwsh(t, "count", "(Get-ScheduledTask | Measure-Object).Count\n", false)
	runPwsh(t, "count", "(Get-ScheduledTask | Measure-Object).Count\n", true)
}
