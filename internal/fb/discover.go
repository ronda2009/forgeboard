package fb

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type Model struct {
	Key     string `json:"key"`
	Name    string `json:"name"`
	GGUF    string `json:"gguf,omitempty"`
	Need    int    `json:"need"` // MiB 估算
	Engine  string `json:"engine"`
	Ctx     int    `json:"ctx,omitempty"`
	Port    int    `json:"port,omitempty"`
	Running bool   `json:"running"`
	VL      bool   `json:"vl,omitempty"`
	Embed   bool   `json:"embed,omitempty"` // 嵌入模型（要 --embeddings 才支持 /v1/embeddings）

	Tier     string `json:"tier,omitempty"`     // 路由器认的档名（原样存，Key 是不可逆折叠）
	Measured bool   `json:"measured,omitempty"` // need 来自实测校准（不是体积估算）
	Source   string `json:"source"`             // scan:<目录> | engine:<名> | port:<n>
}

func ScanGGUF(dir string) []Model {
	var out []Model
	var walk func(string, int)
	walk = func(root string, depth int) {
		if depth > 2 || len(out) > 400 {
			return
		}
		ents, err := os.ReadDir(root)
		if err != nil {
			return
		}
		for _, e := range ents {
			full := filepath.Join(root, e.Name())
			if e.IsDir() {
				walk(full, depth+1)
				continue
			}
			if !strings.EqualFold(filepath.Ext(e.Name()), ".gguf") {
				continue
			}
			low := strings.ToLower(e.Name())
			if strings.Contains(low, "mmproj") {
				continue
			}
			st, err := os.Stat(full)
			if err != nil || st.Size() < 200*1024*1024 {
				continue
			}
			gib := float64(st.Size()) / (1024 * 1024 * 1024)
			vl, emb := false, false
			if kind, ok := KindFromGGUF(full); ok { // 类型读文件自己的元数据（不靠名字猜）
				vl, emb = kind == "vision", kind == "embed"
			} else {
				vl = reVL.MatchString(e.Name()) // 读不了（坏文件）才退回文件名规则
				emb = strings.Contains(low, "embed")
			}
			out = append(out, Model{
				Key:    ModelKey(e.Name()),
				Name:   strings.TrimSuffix(e.Name(), filepath.Ext(e.Name())),
				GGUF:   e.Name(),
				Need:   int(gib*1024) + 2048,
				Engine: "llama",
				Ctx:    pick(emb, 2048, 32768),
				VL:     vl,
				Embed:  emb,
				Source: "scan:" + filepath.Base(dir),
			})
		}
	}
	walk(dir, 0)
	return out
}

var keyRe = regexp.MustCompile(`[^A-Za-z0-9]+`)
var reVL = regexp.MustCompile(`(?i)(vl|vision|clip)`)

func ModelKey(file string) string {
	file = filepath.Base(strings.ReplaceAll(file, "\\", "/"))
	k := strings.TrimSuffix(strings.ToLower(file), ".gguf")
	k = strings.Trim(keyRe.ReplaceAllString(k, "-"), "-")
	if len(k) > 28 {
		k = strings.Trim(k[:28], "-")
	}
	if k == "" {
		k = "model"
	}
	return k
}

func pick(cond bool, a, b int) int {
	if cond {
		return a
	}
	return b
}

type httpResp struct {
	code int
	body []byte
}

func getJSON(ctx context.Context, url string) (*httpResp, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	c := &http.Client{Timeout: 2 * time.Second}
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return &httpResp{resp.StatusCode, b}, nil
}

type RouterStatus struct {
	Current string         `json:"current"`
	PID     int            `json:"pid"`
	Tiers   map[string]int `json:"tiers"`
}

func RouterService(ctx context.Context, base string) string {
	r, err := getJSON(ctx, strings.TrimRight(base, "/")+"/health")
	if err != nil || r.code != 200 {
		return ""
	}
	var h struct {
		Service string `json:"service"`
	}
	if json.Unmarshal(r.body, &h) != nil {
		return ""
	}
	return strings.TrimSpace(h.Service)
}

func AskRouter(ctx context.Context, base string) *RouterStatus {
	r, err := getJSON(ctx, base+"/__status")
	if err != nil || r.code != 200 {
		return nil
	}
	var s RouterStatus
	if json.Unmarshal(r.body, &s) != nil || len(s.Tiers) == 0 {
		return nil
	}
	return &s
}

type RunningService struct {
	Port   int
	Kind   string // llama | strata-router | other
	Model  string // llama-server /health 的 model 字段
	Tiers  map[string]int
	Router bool
}

func ProbePort(ctx context.Context, port int) *RunningService {
	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	if r, err := getJSON(ctx, base+"/__status"); err == nil && r.code == 200 {
		var s RouterStatus
		if json.Unmarshal(r.body, &s) == nil && len(s.Tiers) > 0 {
			return &RunningService{Port: port, Kind: "strata-router", Tiers: s.Tiers, Router: true}
		}
	}
	if r, err := getJSON(ctx, base+"/health"); err == nil && r.code == 200 {
		var h struct {
			Model string `json:"model"`
		}
		if json.Unmarshal(r.body, &h) == nil && h.Model != "" {
			return &RunningService{Port: port, Kind: "llama", Model: h.Model}
		}
	}
	if r, err := getJSON(ctx, base+"/v1/models"); err == nil && r.code == 200 {
		var v struct {
			Data []struct {
				ID string `json:"id"`
			} `json:"data"`
		}
		if json.Unmarshal(r.body, &v) == nil && len(v.Data) > 0 {
			return &RunningService{Port: port, Kind: "other", Model: v.Data[0].ID}
		}
	}
	return nil
}

func ProbePorts(ctx context.Context, ports []int) []RunningService {
	type res struct{ svc *RunningService }
	ch := make(chan res, len(ports))
	cx, cancel := context.WithCancel(ctx)
	defer cancel()
	for _, p := range ports {
		go func(p int) {
			svc := ProbePort(cx, p)
			select {
			case ch <- res{svc}:
			case <-cx.Done():
			}
		}(p)
	}
	var out []RunningService
	done := 0
	for done < len(ports) {
		select {
		case r := <-ch:
			if r.svc != nil {
				out = append(out, *r.svc)
			}
			done++ // 没服务的也算一条，否则又要等超时
		case <-time.After(3 * time.Second):
			return out
		}
	}
	return out
}

func ParsePortRange(spec string) []int {
	var out []int
	for _, part := range strings.Split(spec, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if lo, hi, ok := strings.Cut(part, "-"); ok {
			a, _ := strconv.Atoi(strings.TrimSpace(lo))
			b, _ := strconv.Atoi(strings.TrimSpace(hi))
			for p := a; p <= b && p-a < 256; p++ {
				out = append(out, p)
			}
			continue
		}
		if p, err := strconv.Atoi(part); err == nil {
			out = append(out, p)
		}
	}
	return out
}
