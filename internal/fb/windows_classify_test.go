package fb

import "testing"

// 对话窗口判定：渠道来的、控制台新建的、主会话都算对话；定时任务与心跳算系统
func TestClassifyWindows(t *testing.T) {
	cases := []struct {
		name  string
		s     map[string]any
		kind  string
		where string
	}{
		{"feishu group", map[string]any{"key": "agent:main:feishu:group:oc_x", "channel": "feishu", "peerKind": "group", "createdVia": "channel"}, "chat", "where-feishu-group"},
		{"wechat direct", map[string]any{"key": "agent:main:openclaw-weixin:direct:o9", "channel": "openclaw-weixin", "createdVia": "channel"}, "chat", "where-openclaw-weixin"},
		{"console new window", map[string]any{"key": "agent:main:dashboard:cfa3032f", "label": "小杜全景声Q", "createdVia": "operator"}, "chat", "where-dashboard"},
		{"main session", map[string]any{"key": "agent:main:main", "isMain": true}, "chat", "where-main"},
		{"cron job", map[string]any{"key": "agent:main:cron:fea31b52", "label": "Automation: 每周技能清理", "createdVia": "cron"}, "sys", "where-other"},
		{"heartbeat", map[string]any{"key": "agent:main:main:heartbeat", "label": "心跳工作间"}, "sys", "where-other"},
		{"cron trigger", map[string]any{"key": "agent:main:cron:562fdc08:trigger"}, "sys", "where-other"},
	}
	for _, c := range cases {
		if got := ocClassify(c.s); got != c.kind {
			t.Errorf("%s: kind=%s, want %s", c.name, got, c.kind)
		}
		if got := ocWhere(c.s); got != c.where {
			t.Errorf("%s: where=%s, want %s", c.name, got, c.where)
		}
	}
}
