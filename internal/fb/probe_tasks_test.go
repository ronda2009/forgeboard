package fb

import (
	"context"
	"testing"
	"time"
)

func TestProbeTasks(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	t0 := time.Now()
	rows, err := TaskList(ctx)
	t.Logf("elapsed=%s err=%v rows=%d", time.Since(t0).Round(time.Millisecond), err, len(rows))
	for i, r := range rows {
		if i >= 6 {
			break
		}
		t.Logf("ROW %d name=%q sys=%v hid=%v en=%v st=%q trig=%q last=%d res=%q note=%q",
			i, r.Name, r.System, r.Hidden, r.Enabled, r.State, r.Trigger, r.LastRun, r.Result, r.ResultNote)
	}
}
