//go:build !windows

package app

func startTrayImpl(a *App, iconBytes []byte) error { return nil }

func trayIconPath() string { return "" }

func trayAvailable() bool { return false }

func trayHideWindow(a *App) {}

func trayQuitNow() {}

func trayRefreshTip() {}
