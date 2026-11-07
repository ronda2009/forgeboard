//go:build !windows

package fb

import (
	"context"
	"errors"
)

type TaskItem struct {
	Name       string `json:"name"`
	Path       string `json:"path"`
	System     bool   `json:"system"`
	Hidden     bool   `json:"hidden"`
	Enabled    bool   `json:"enabled"`
	State      string `json:"state"`
	Trigger    string `json:"trigger"`
	LastRun    int64  `json:"lastRun"`
	Result     string `json:"result"`
	ResultNote string `json:"resultNote,omitempty"`
}

func TaskList(ctx context.Context) ([]TaskItem, error) {
	return nil, errors.New("scheduled tasks are a Windows thing; nothing to list here")
}
