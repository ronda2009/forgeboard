package app

import (
	"errors"
	"fmt"
	"os/exec"
	"runtime"

	"github.com/ronda2009/forgeboard/internal/fb"
)

func (a *App) StartupSwitchItem(source, name string, enable, dry bool) (*fb.OpResult, error) {
	res, err := fb.StartupSwitch(source, name, enable, dry)
	a.push(fmt.Sprintf("startup switch %s %s enable=%v dry=%v err=%v", source, name, enable, dry, err))
	return res, err
}

func (a *App) StartupDeleteItem(source, name string, dry bool) (*fb.OpResult, error) {
	res, err := fb.StartupDelete(source, name, dry)
	a.push(fmt.Sprintf("startup delete %s %s dry=%v err=%v", source, name, dry, err))
	return res, err
}

func (a *App) SetSelfStart(on, dry bool) (*fb.OpResult, error) {
	res, err := fb.SelfStart(on, dry)
	a.push(fmt.Sprintf("self start on=%v dry=%v err=%v", on, dry, err))
	return res, err
}

func (a *App) OpenStartupFolder() error {
	dir := fb.StartupFolderUser()
	if dir == "" {
		return errors.New("no startup folder on this system")
	}
	if runtime.GOOS != "windows" {
		return errors.New("no startup folder on this system")
	}
	return exec.Command("explorer.exe", dir).Start()
}
