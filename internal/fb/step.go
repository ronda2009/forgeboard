package fb

//

import (
	"strings"
	"sync/atomic"
)

var progressFn atomic.Value // func(string)

func SetProgress(f func(string)) {
	if f != nil {
		progressFn.Store(f)
	}
}

func Step(token string, args ...string) {
	msg := "step " + token + argPairs(args)
	LogAction(msg)
	if v := progressFn.Load(); v != nil {
		if f, ok := v.(func(string)); ok {
			f(msg)
		}
	}
}

func argPairs(args []string) string {
	if len(args) == 0 {
		return ""
	}
	var b strings.Builder
	for i := 0; i+1 < len(args); i += 2 {
		b.WriteString(" " + args[i] + "=" + args[i+1])
	}
	return b.String()
}
