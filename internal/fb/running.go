package fb

//
//

import (
	"context"
	"time"
)

func PortsUp(ctx context.Context, ports []int) map[int]bool {
	out := map[int]bool{}
	if len(ports) == 0 {
		return out
	}
	type res struct {
		port int
		up   bool
	}
	ch := make(chan res, len(ports))
	cx, cancel := context.WithTimeout(ctx, 1*time.Second)
	defer cancel()
	for _, p := range ports {
		go func(p int) {
			up := PortUp(p)
			select {
			case ch <- res{p, up}:
			case <-cx.Done():
			}
		}(p)
	}
	for done := 0; done < len(ports); done++ {
		select {
		case r := <-ch:
			out[r.port] = r.up
		case <-cx.Done():
			return out
		}
	}
	return out
}
