package fb

//

import (
	"os"
	"path/filepath"
)

type Service struct {
	Name      string         `json:"name"`
	Kind      string         `json:"kind"` // gateway | router | adapter | engine | api
	Port      int            `json:"port,omitempty"`
	URL       string         `json:"url,omitempty"`
	Up        bool           `json:"up"`
	Installed *bool          `json:"installed,omitempty"` // nil = 不判装没装（装了就能跑的那种）
	Where     ServiceLocation `json:"where"`
	Ctl       ServiceControl  `json:"ctl"`
	Note      string          `json:"note,omitempty"`
}

func CfgHasSwap() bool {
	_, err := os.Stat(filepath.Join(CfgDir(), "llama-swap.exe"))
	return err == nil
}
