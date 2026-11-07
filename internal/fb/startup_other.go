//go:build !windows

package fb

import "errors"

type StartupItem struct {
	Name      string `json:"name"`
	Source    string `json:"source"`
	Command   string `json:"command"`
	Enabled   bool   `json:"enabled"`
	Editable  bool   `json:"editable"`
	Deletable bool   `json:"deletable"`
	Note      string `json:"note"`
}

func StartupList() []StartupItem { return nil }

func StartupSwitch(source, name string, enable, dry bool) (*OpResult, error) {
	return nil, errors.New("startup items are a Windows thing; nothing to manage here")
}

func StartupDelete(source, name string, dry bool) (*OpResult, error) {
	return nil, errors.New("startup items are a Windows thing; nothing to manage here")
}

func SelfStartState() (bool, string) { return false, "" }

func SelfStart(on, dry bool) (*OpResult, error) {
	return nil, errors.New("startup items are a Windows thing; nothing to manage here")
}

func StartupFolderUser() string { return "" }
