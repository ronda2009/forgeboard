package fb

import (
	"os/exec"
	"runtime"
	"strconv"
	"strings"
)

type GPU struct {
	Index int
	Name  string
	Total int // MiB
	Free  int // MiB
}

func GPUs() ([]GPU, error) {
	cmd := exec.Command("nvidia-smi", "--query-gpu=index,name,memory.total,memory.free", "--format=csv,noheader,nounits")
	if runtime.GOOS == "windows" {
		cmd.SysProcAttr = hideCmd
	}
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	var gpus []GPU
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		f := strings.Split(line, ",")
		if len(f) < 4 {
			continue
		}
		idx, _ := strconv.Atoi(strings.TrimSpace(f[0]))
		total, _ := strconv.Atoi(strings.TrimSpace(f[2]))
		free, _ := strconv.Atoi(strings.TrimSpace(f[3]))
		gpus = append(gpus, GPU{Index: idx, Name: strings.TrimSpace(f[1]), Total: total, Free: free})
	}
	return gpus, nil
}
