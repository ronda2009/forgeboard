//go:build windows

package fb

import "syscall"

var hideCmd = &syscall.SysProcAttr{HideWindow: true}

var detachCmd = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x10 | 0x200}
