package fb

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

type PowerRow struct {
	Index     int    `json:"index"`
	Name      string `json:"name"`
	Current   int    `json:"current"`
	Default   int    `json:"default"`
	Min       int    `json:"min"`
	Max       int    `json:"max"`
	Supported bool   `json:"supported"` // 这张卡认不认功率上限（读不到就是不支持/非 N 卡）
	Note      string `json:"note,omitempty"`
}

func smi(ctx context.Context, args ...string) (string, error) {
	cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, "nvidia-smi", args...)
	if runtime.GOOS == "windows" {
		cmd.SysProcAttr = hideCmd
	}
	out, err := cmd.CombinedOutput()
	txt := strings.TrimSpace(string(out))
	if err != nil {
		if txt == "" {
			return "", fmt.Errorf("nvidia-smi failed: %w", err)
		}
		return txt, fmt.Errorf("%s", txt)
	}
	return txt, nil
}

func Powers(ctx context.Context) []PowerRow {
	cards, err := GPUs()
	if err != nil {
		return []PowerRow{{Index: -1, Name: "nvidia-smi", Supported: false, Note: err.Error()}}
	}
	var out []PowerRow
	for _, c := range cards {
		r := PowerRow{Index: c.Index, Name: c.Name}
		q, err := smi(ctx, "-i", itoa(c.Index), "--query-gpu=power.limit,power.default_limit,power.min_limit,power.max_limit", "--format=csv,noheader,nounits")
		if err != nil || strings.Contains(q, "Err") {
			r.Note = strings.TrimSpace(errText(err, q))
			out = append(out, r)
			continue
		}
		vals := strings.Split(q, ",")
		if len(vals) < 4 {
			r.Note = "cannot read power limits"
			out = append(out, r)
			continue
		}
		r.Current, r.Default, r.Min, r.Max = atoi(vals[0]), atoi(vals[1]), atoi(vals[2]), atoi(vals[3])
		r.Supported = r.Max > 0
		if !r.Supported {
			r.Note = "this card does not report a power limit"
		}
		out = append(out, r)
	}
	return out
}

func errText(err error, extra string) string {
	if err != nil {
		return err.Error()
	}
	return extra
}

func atoi(s string) int {
	s = strings.TrimSpace(s)
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return n
		}
		n = n*10 + int(c-'0')
	}
	return n
}

func SetPower(ctx context.Context, index, watts int, dry bool) (string, error) {
	q, err := smi(ctx, "-i", itoa(index), "--query-gpu=power.limit,power.default_limit,power.min_limit,power.max_limit", "--format=csv,noheader,nounits")
	if err != nil {
		return "", fmt.Errorf("cannot read power limits for GPU %d: %s", index, errText(err, q))
	}
	vals := strings.Split(q, ",")
	if len(vals) < 4 {
		return "", fmt.Errorf("cannot read power limits for GPU %d", index)
	}
	cur, def, mn, mx := atoi(vals[0]), atoi(vals[1]), atoi(vals[2]), atoi(vals[3])
	if mx <= 0 {
		return "", fmt.Errorf("GPU %d does not report a power limit (nothing to set)", index)
	}
	if dry && watts <= 0 {
		return fmt.Sprintf("[DryRun] GPU %d: limit %d W (default %d, settable %d-%d)", index, cur, def, mn, mx), nil
	}
	if watts < mn || watts > mx {
		return "", fmt.Errorf("%d W is outside the settable range %d-%d W (nothing changed)", watts, mn, mx)
	}
	head := fmt.Sprintf("GPU %d: limit %d W (default %d, settable %d-%d) -> %d W", index, cur, def, mn, mx, watts)
	if dry {
		return "[DryRun] " + head, nil
	}
	out, err := smi(ctx, "-i", itoa(index), "--power-limit="+itoa(watts))
	if err != nil {
		if strings.Contains(out, "Insufficient Permissions") || strings.Contains(out, "Insufficient Permissions") {
			return "need-elevation: writing power limits needs administrator rights (double-click launch runs as a normal user)", nil
		}
		return "", fmt.Errorf("set power limit failed: %s", errText(err, out))
	}
	back, err := smi(ctx, "-i", itoa(index), "--query-gpu=power.limit", "--format=csv,noheader,nounits")
	if err != nil {
		return head + " (read-back failed: " + errText(err, back) + ")", nil
	}
	return fmt.Sprintf("%s - read back: %d W", head, atoi(back)), nil
}

func SetPowerElevated(ctx context.Context, index, watts int) (string, error) {
	if watts <= 0 {
		return "", errors.New("no wattage given")
	}
	var name string
	var args []string
	if runtime.GOOS == "windows" {
		name = "powershell"
		args = []string{"-NoProfile", "-Command", fmt.Sprintf("Start-Process -FilePath 'nvidia-smi' -Verb RunAs -Wait -WindowStyle Hidden -ArgumentList '-i','%d','--power-limit=%d'", index, watts)}
	} else {
		if _, err := exec.LookPath("pkexec"); err != nil {
			return "", errors.New("no way to elevate on this system (install polkit/pkexec or run forgeboard as root)")
		}
		name = "pkexec"
		args = []string{"nvidia-smi", "-i", itoa(index), fmt.Sprintf("--power-limit=%d", watts)}
	}
	cctx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, name, args...)
	if runtime.GOOS == "windows" {
		cmd.SysProcAttr = hideCmd
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		txt := strings.TrimSpace(string(out))
		if strings.Contains(txt, "操作已被用户取消") || strings.Contains(strings.ToLower(txt), "cancel") {
			return "", errors.New("UAC prompt was cancelled (nothing changed)")
		}
		return "", fmt.Errorf("elevated retry failed: %s", firstNonEmpty(txt, err.Error()))
	}
	back, err := smi(ctx, "-i", itoa(index), "--query-gpu=power.limit", "--format=csv,noheader,nounits")
	if err != nil {
		return fmt.Sprintf("GPU %d set to %d W (read-back failed: %s)", index, watts, errText(err, back)), nil
	}
	return fmt.Sprintf("GPU %d set to %d W - read back: %d W", index, watts, atoi(back)), nil
}
