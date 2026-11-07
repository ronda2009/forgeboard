package app

import "sync"

var (
	langMu   sync.RWMutex
	uiLangGo = "en"
)

func uiT(en string) string {
	langMu.RLock()
	lang := uiLangGo
	langMu.RUnlock()
	if lang != "zh" {
		return en
	}
	if zh, ok := dictZH[en]; ok {
		return zh
	}
	return en
}

func SetUILang(lang string) {
	langMu.Lock()
	if lang == "zh" {
		uiLangGo = "zh"
	} else {
		uiLangGo = "en"
	}
	langMu.Unlock()
	trayRefreshTip()
}

var dictZH = map[string]string{
	" | left-click brings the window back, right-click opens the menu":                       "｜左键叫回主界面，右键菜单",
	"tray icon is up: left-click brings the window back, right-click opens the menu":         "已在任务栏图标区常驻：左键叫回主界面，右键出菜单",
	"tray could not start (message window failed): the X button will quit instead of hiding": "托盘起不来：建消息窗失败（点右上角叉会直接退出，不会藏窗口）",
	"tray icon could not be loaded (window and features are fine, just no tray icon)":        "托盘图标读不出来（窗口与功能照常，只是任务栏里没图标）",
	"main session":         "主会话",
	"feishu group":         "飞书群",
	"feishu direct":        "飞书私聊",
	"webchat":              "网页对话",
	"wechat":               "微信",
	"Open main window":     "打开主界面",
	"Open Models":          "打开「模型」",
	"Open Sessions & Jobs": "打开「窗口与任务」",
	"Open System":          "打开「系统」",
	"Open Diagnostics":     "打开「自检」",
	"Measure VRAM now":     "立刻量一次显存",
	"Quit":                 "退出",
	"Hidden into the tray. Double-click the icon to bring me back.": "已收到任务栏图标里，随时叫我：双击图标就回来。",
}
