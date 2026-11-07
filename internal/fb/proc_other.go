//go:build !windows

package fb

import "syscall"

var hideCmd = &syscall.SysProcAttr{}

var detachCmd = &syscall.SysProcAttr{}
