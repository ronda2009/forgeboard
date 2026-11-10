package fb

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
)

type Paths struct {
	LLAMA_EXE      string `json:"LLAMA_EXE"`
	MODELS         string `json:"MODELS"`
	PY             string `json:"PY"`
	SERVE          string `json:"SERVE"` // strata server.py
	TIER_DIR       string `json:"TIER_DIR"`
	OC_HOME        string `json:"OC_HOME"`   // OpenClaw 状态目录（M3 用）
	OC_CFG         string `json:"OC_CFG"`    // openclaw.json
	OC_CLI         string `json:"OC_CLI"`    // openclaw.mjs 路径（没填走 3.x 的候选路径）
	GW_RESCUE      string `json:"GW_RESCUE"` // 网关救援脚本（gateway-rescue.ps1）；没填就说没填，不猜
	Lang           string `json:"Lang"`      // 界面语言（en|zh）；存这里才能重启后还是中文（浏览器缓存不可靠）
	RouterURL      string `json:"RouterURL"`
	SwapPort       int    `json:"SwapPort"` // llama-swap 管理端口
	CandidatePorts []int  `json:"CandidatePorts"`
	GPU            string `json:"GPU"`       // 模型用哪张显卡：空/auto=按空闲自动挑，0/1=指定物理编号
	HideSmall      bool   `json:"HideSmall"` // 模型页勾选「隐藏嵌入与视觉模型」（存这里，重启还记得）
}

func CfgDir() string {
	exe, _ := os.Executable()
	if exe != "" {
		if _, err := os.Stat(filepath.Join(filepath.Dir(exe), "forgeboard.json")); err == nil {
			return filepath.Dir(exe)
		}
	}
	base := ""
	if runtime.GOOS == "windows" {
		base = os.Getenv("LOCALAPPDATA")
	} else {
		base = os.Getenv("XDG_CONFIG_HOME")
		if base == "" {
			if home, err := os.UserHomeDir(); err == nil {
				base = filepath.Join(home, ".config")
			}
		}
	}
	if base == "" {
		base = "."
	}
	d := filepath.Join(base, "forgeboard")
	_ = os.MkdirAll(d, 0o755)
	return d
}

func LoadPaths() Paths {
	var p Paths
	if b, err := os.ReadFile(filepath.Join(CfgDir(), "forgeboard.json")); err == nil {
		_ = json.Unmarshal(b, &p)
	}
	if p.RouterURL == "" {
		p.RouterURL = "http://127.0.0.1:8080"
	}
	if p.SwapPort == 0 {
		p.SwapPort = 8089 // 端口登记表（docs\端口登记表.md）里 8089 未占；冲突就在 forgeboard.json 改
	}
	return p
}
