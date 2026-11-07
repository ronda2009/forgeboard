package fb

type OpResult struct {
	Token string            `json:"token"`      // 文案键（界面按它查文案）
	Dry   bool              `json:"dry"`        // DryRun：界面加「没动服务」标头
	Args  map[string]string `json:"args"`       // 文案参数（count/first/later/stamp/tier/model…）
	Rows  []ReserveRow      `json:"rows"`       // DryRun 时列的档位现值表
	Bad   []string          `json:"bad"`        // 写完读回不符的明细（文件 现值）
	Note  string            `json:"note"`       // 兜底原文（救援脚本输出、HTTP 返回等）
}

func op(token string, dry bool) *OpResult {
	return &OpResult{Token: token, Dry: dry, Args: map[string]string{}}
}

func (r *OpResult) withNote(s string) *OpResult {
	r.Note = s
	return r
}
func (r *OpResult) subj(kv ...string) *OpResult {
	if r.Args == nil {
		r.Args = map[string]string{}
	}
	for i := 0; i+1 < len(kv); i += 2 {
		r.Args[kv[i]] = kv[i+1]
	}
	return r
}
