//go:build windows

package app

import (
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

var (
	user32         = syscall.NewLazyDLL("user32.dll")
	kernel32       = syscall.NewLazyDLL("kernel32.dll")
	shell32        = syscall.NewLazyDLL("shell32.dll")
	procRegCls     = user32.NewProc("RegisterClassExW")
	procCreateWin  = user32.NewProc("CreateWindowExW")
	procDefProc    = user32.NewProc("DefWindowProcW")
	procQuitMsg    = user32.NewProc("PostQuitMessage")
	procGetMsg     = user32.NewProc("GetMessageW")
	procTransMsg   = user32.NewProc("TranslateMessage")
	procDispMsg    = user32.NewProc("DispatchMessageW")
	procLoadImg    = user32.NewProc("LoadImageW")
	procSysMetric  = user32.NewProc("GetSystemMetrics")
	procNewMenu    = user32.NewProc("CreatePopupMenu")
	procAddMenu    = user32.NewProc("AppendMenuW")
	procKillMenu   = user32.NewProc("DestroyMenu")
	procTrackMenu  = user32.NewProc("TrackPopupMenu")
	procCursorPos  = user32.NewProc("GetCursorPos")
	procSetFront   = user32.NewProc("SetForegroundWindow")
	procRegWmMsg   = user32.NewProc("RegisterWindowMessageW")
	procPostMsg    = user32.NewProc("PostMessageW")
	procNotifyIcon = shell32.NewProc("Shell_NotifyIconW")
	procModHandle  = kernel32.NewProc("GetModuleHandleW")
)

const (
	wmDestroy     = 0x0002
	wmClose       = 0x0010
	wmContextMenu = 0x007B
	wmLButtonUp   = 0x0202
	wmLButtonDbl  = 0x0203
	wmRButtonUp   = 0x0204
	wmTrayCb      = 0x0401 // WM_APP(0x0400) + 1：托盘图标回给我们的消息

	nimAdd    = 0x0
	nimModify = 0x1
	nimDelete = 0x4

	nifMessage = 0x1
	nifIcon    = 0x2
	nifTip     = 0x4
	nifInfo    = 0x10

	niifInfo    = 0x1
	niifNoSound = 0x10

	imageIcon      = 1
	lrLoadFromFile = 0x10
	lrDefaultSize  = 0x40
	smCxSmIcon     = 49
	smCySmIcon     = 50

	tpmLeftAlign  = 0x0
	tpmRightBtn   = 0x2
	tpmBottomAlgn = 0x20
	tpmReturnCmd  = 0x100

	mfString    = 0x0
	mfSeparator = 0x800

	idMenuOpen        = 1

	idMenuQuit        = 3
	idMenuTabModels   = 11
	idMenuTabSessions = 12
	idMenuTabSystem   = 13
	idMenuTabDiag     = 14

	hwndMessage = ^uintptr(2) // HWND_MESSAGE = (HWND)-3：只收消息的隐形父窗
	trayIconID  = 1
)

type pointWin struct{ x, y int32 }

type wndClassExW struct {
	size          uint32
	style         uint32
	lpfnWndProc   uintptr
	cbClsExtra    int32
	cbWndExtra    int32
	hInstance     uintptr
	hIcon         uintptr
	hCursor       uintptr
	hBackground   uintptr
	lpszMenuName  *uint16
	lpszClassName *uint16
	hIconSm       uintptr
}

type msgW struct {
	hwnd     uintptr
	message  uint32
	wParam   uintptr
	lParam   uintptr
	time     uint32
	pt       pointWin
	lPrivate uint32
}

type notifyIconDataW struct {
	size         uint32
	hWnd         uintptr
	id           uint32
	flags        uint32
	callbackMsg  uint32
	hIcon        uintptr
	tip          [128]uint16
	state        uint32
	stateMask    uint32
	info         [256]uint16
	timeout      uint32 // Vista 之后是保留字段（与 uVersion 共用），填 0 用系统默认时长
	infoTitle    [64]uint16
	infoFlags    uint32
	guidItem     [16]byte
	hBalloonIcon uintptr
}

type trayState struct {
	mu        sync.Mutex
	hwnd      uintptr
	nid       notifyIconDataW
	hIcon     uintptr
	wmTaskbar uint32
}

var tray trayState

func trayIconPath() string { return filepath.Join(os.TempDir(), "forgeboard-tray.ico") }

func startTrayImpl(a *App, iconBytes []byte) error {
	if len(iconBytes) == 0 {
		return os.ErrInvalid
	}
	tmp := trayIconPath()
	if err := os.WriteFile(tmp, iconBytes, 0o644); err != nil {
		return err
	}
	go runTray(a, tmp)
	return nil
}

func trayAvailable() bool {
	tray.mu.Lock()
	defer tray.mu.Unlock()
	return tray.hwnd != 0
}

func trayHWND() uintptr {
	tray.mu.Lock()
	defer tray.mu.Unlock()
	return tray.hwnd
}

func utf16Ptr(s string) *uint16 {
	p, _ := syscall.UTF16PtrFromString(s)
	return p
}

func copyUtf16(dst []uint16, s string) {
	b, err := syscall.UTF16FromString(s)
	if err != nil {
		return
	}
	copy(dst, b)
}

func runTray(a *App, icoPath string) {
	runtime.LockOSThread()

	className := utf16Ptr("ForgeBoardTrayWindow")
	wndName := utf16Ptr("ForgeBoardTrayMessage")

	wndProc := syscall.NewCallback(func(hwnd uintptr, msg uint32, wParam, lParam uintptr) uintptr {
		switch msg {
		case wmTrayCb:
			switch uint32(lParam & 0xffff) {
			case wmLButtonUp, wmLButtonDbl:
				a.ShowMainWindow()
			case wmRButtonUp, wmContextMenu:
				showTrayMenu(a, hwnd)
			}
			return 0
		case wmDestroy:
			trayRemove()
			procQuitMsg.Call(0)
			return 0
		}
		if tb := tray.wmTaskbar; tb != 0 && msg == tb { // 资源管理器重启后托盘图标会消失，重新登记一次
			trayAdd()
			return 0
		}
		r, _, _ := procDefProc.Call(hwnd, uintptr(msg), wParam, lParam)
		return r
	})

	var wc wndClassExW
	wc.size = uint32(unsafe.Sizeof(wc))
	wc.lpfnWndProc = wndProc
	wc.hInstance, _, _ = procModHandle.Call(0)
	wc.lpszClassName = className
	procRegCls.Call(uintptr(unsafe.Pointer(&wc)))

	hwnd, _, _ := procCreateWin.Call(
		0, uintptr(unsafe.Pointer(className)), uintptr(unsafe.Pointer(wndName)),
		0, 0, 0, 0, 0,
		uintptr(hwndMessage), 0, wc.hInstance, 0,
	)
	if hwnd == 0 {
		a.push("tray could not start (message window failed): the X button will quit instead of hiding")
		return
	}
	tray.mu.Lock()
	tray.hwnd = hwnd
	tray.mu.Unlock()

	tb, _, _ := procRegWmMsg.Call(uintptr(unsafe.Pointer(utf16Ptr("TaskbarCreated"))))
	tray.wmTaskbar = uint32(tb)

	cx, _, _ := procSysMetric.Call(smCxSmIcon)
	cy, _, _ := procSysMetric.Call(smCySmIcon)
	h, _, _ := procLoadImg.Call(0, uintptr(unsafe.Pointer(utf16Ptr(icoPath))), imageIcon, cx, cy, lrLoadFromFile)
	if h == 0 {
		h, _, _ = procLoadImg.Call(0, uintptr(unsafe.Pointer(utf16Ptr(icoPath))), imageIcon, 0, 0, lrLoadFromFile|lrDefaultSize)
	}
	if h == 0 {
		a.push("tray icon could not be loaded (window and features are fine, just no tray icon)")
		return
	}
	tray.mu.Lock()
	tray.hIcon = h
	tray.mu.Unlock()

	trayAdd()
	a.push("tray icon is up: left-click brings the window back, right-click opens the menu")

	var m msgW
	for {
		r, _, _ := procGetMsg.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 { // 0＝收到 WM_QUIT；-1＝出错
			break
		}
		procTransMsg.Call(uintptr(unsafe.Pointer(&m)))
		procDispMsg.Call(uintptr(unsafe.Pointer(&m)))
	}
	trayRemove()
}

const trayBaseFlags = nifMessage | nifIcon | nifTip

func trayAdd() {
	tray.mu.Lock()
	defer tray.mu.Unlock()
	if tray.hwnd == 0 {
		return
	}
	nid := &tray.nid
	*nid = notifyIconDataW{}
	nid.size = uint32(unsafe.Sizeof(*nid))
	nid.hWnd = tray.hwnd
	nid.id = trayIconID
	nid.hIcon = tray.hIcon
	nid.flags = trayBaseFlags
	nid.callbackMsg = wmTrayCb
	copyUtf16(nid.tip[:], trayTitle+uiT(" | left-click brings the window back, right-click opens the menu"))
	procNotifyIcon.Call(nimAdd, uintptr(unsafe.Pointer(nid)))
}

func trayRefreshTip() {
	tray.mu.Lock()
	if tray.hwnd == 0 {
		tray.mu.Unlock()
		return
	}
	nid := &tray.nid
	copyUtf16(nid.tip[:], trayTitle+uiT(" | left-click brings the window back, right-click opens the menu"))
	nid2 := *nid
	tray.mu.Unlock()
	nid2.flags = trayBaseFlags
	nid2.size = uint32(unsafe.Sizeof(nid2))
	procNotifyIcon.Call(nimModify, uintptr(unsafe.Pointer(&nid2)))
}

func trayRemove() {
	tray.mu.Lock()
	nid := tray.nid
	hwnd := tray.hwnd
	tray.mu.Unlock()
	if hwnd == 0 {
		return
	}
	nid.flags = 0
	nid.size = uint32(unsafe.Sizeof(nid))
	procNotifyIcon.Call(nimDelete, uintptr(unsafe.Pointer(&nid)))
}

func trayBalloon(title, body string) {
	tray.mu.Lock()
	if tray.hwnd == 0 {
		tray.mu.Unlock()
		return
	}
	nid := tray.nid
	tray.mu.Unlock()

	nid.flags = nifInfo | nifIcon
	nid.hIcon = tray.hIcon
	copyUtf16(nid.infoTitle[:], title)
	copyUtf16(nid.info[:], body)
	nid.infoFlags = niifInfo | niifNoSound
	nid.timeout = 0
	nid.size = uint32(unsafe.Sizeof(nid))
	procNotifyIcon.Call(nimModify, uintptr(unsafe.Pointer(&nid)))

	go func() {
		time.Sleep(8 * time.Second)
		tray.mu.Lock()
		if tray.hwnd == 0 {
			tray.mu.Unlock()
			return
		}
		nid := tray.nid
		tray.mu.Unlock()
		nid.flags = nifInfo
		copyUtf16(nid.info[:], "")
		nid.size = uint32(unsafe.Sizeof(nid))
		procNotifyIcon.Call(nimModify, uintptr(unsafe.Pointer(&nid)))
	}()
}

func showTrayMenu(a *App, hwnd uintptr) {
	hMenu, _, _ := procNewMenu.Call()
	if hMenu == 0 {
		return
	}
	defer procKillMenu.Call(hMenu)

	procAddMenu.Call(hMenu, mfString, idMenuOpen, uintptr(unsafe.Pointer(utf16Ptr(uiT("Open main window")))))
	procAddMenu.Call(hMenu, mfSeparator, 0, 0)
	procAddMenu.Call(hMenu, mfString, idMenuTabModels, uintptr(unsafe.Pointer(utf16Ptr(uiT("Open Models")))))
	procAddMenu.Call(hMenu, mfString, idMenuTabSessions, uintptr(unsafe.Pointer(utf16Ptr(uiT("Open Sessions & Jobs")))))
	procAddMenu.Call(hMenu, mfString, idMenuTabSystem, uintptr(unsafe.Pointer(utf16Ptr(uiT("Open System")))))
	procAddMenu.Call(hMenu, mfString, idMenuTabDiag, uintptr(unsafe.Pointer(utf16Ptr(uiT("Open Diagnostics")))))
	procAddMenu.Call(hMenu, mfSeparator, 0, 0)

	procAddMenu.Call(hMenu, mfSeparator, 0, 0)
	procAddMenu.Call(hMenu, mfString, idMenuQuit, uintptr(unsafe.Pointer(utf16Ptr(uiT("Quit")))))

	procSetFront.Call(hwnd)
	var pt pointWin
	procCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	picked, _, _ := procTrackMenu.Call(
		hMenu,
		tpmBottomAlgn|tpmLeftAlign|tpmRightBtn|tpmReturnCmd,
		uintptr(pt.x), uintptr(pt.y), 0, hwnd, 0,
	)

	switch int(picked) {
	case idMenuOpen:
		a.ShowMainWindow()
	case idMenuTabModels:
		a.OpenTab("models")
	case idMenuTabSessions:
		a.OpenTab("sessions")
	case idMenuTabSystem:
		a.OpenTab("system")
	case idMenuTabDiag:
		a.OpenTab("diag")

	case idMenuQuit:
		a.QuitApp()
	}
}

func trayHideWindow(a *App) {
	a.HideMainWindow()
	trayBalloon(trayTitle, uiT("Hidden into the tray. Double-click the icon to bring me back."))
}

func trayQuitNow() {
	if hwnd := trayHWND(); hwnd != 0 {
		procPostMsg.Call(hwnd, wmClose, 0, 0)
	}
}
