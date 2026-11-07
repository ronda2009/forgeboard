package fb

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

type Swap struct {
	dir string // 配置目录（llama-swap.exe 与 swap-config.json 都在这）
	url string // http://127.0.0.1:<管理端口>
}

func NewSwap(p Paths) (*Swap, error) {
	dir := CfgDir()
	exe := filepath.Join(dir, "llama-swap.exe")
	if _, err := os.Stat(exe); err != nil {
		if embeddedSwap == nil {
			return nil, fmt.Errorf("no llama-swap.exe here and this binary has no embedded copy (dev build): run tools/fetch-llama-swap.cjs, then go build -tags swapembed")
		}
		if err := os.WriteFile(exe, embeddedSwap, 0o755); err != nil {
			return nil, err
		}
	}
	port := p.SwapPort
	if port == 0 {
		port = 8089
	}
	return &Swap{dir: dir, url: fmt.Sprintf("http://127.0.0.1:%d", port)}, nil
}

//
func (s *Swap) EnsureUp(ctx context.Context, p Paths) error {
	cfg := filepath.Join(s.dir, "swap-config.json")
	if _, err := os.Stat(cfg); err != nil {
		return fmt.Errorf("missing %s (forgectl doctor writes a minimal one)", cfg)
	}
	fp, err := cfgFingerprint(cfg)
	if err != nil {
		return err
	}
	if s.Health(ctx) == nil {
		if sameCfg(s.dir, fp) {
			return nil // 引擎在跑、跑的就是这份配置
		}
		if err := s.Restart(ctx, p); err != nil {
			return err
		}
		return writeCfgState(s.dir, fp)
	}
	if err := s.launch(ctx, p, cfg); err != nil {
		return err
	}
	return writeCfgState(s.dir, fp)
}

func (s *Swap) Restart(ctx context.Context, p Paths) error {
	cfg := filepath.Join(s.dir, "swap-config.json")
	fp, err := cfgFingerprint(cfg)
	if err != nil {
		return err
	}
	if s.Health(ctx) == nil {
		killSwapOnPort(p.SwapPort)
		if !waitSwapDown(ctx, s.url) {
			return fmt.Errorf("llama-swap 还在 %d 上应答，没停下来（没敢再起一份，避免两份引擎抢同一个口）", p.SwapPort)
		}
	}
	if err := s.launch(ctx, p, cfg); err != nil {
		return err
	}
	return writeCfgState(s.dir, fp)
}

func (s *Swap) launch(ctx context.Context, p Paths, cfg string) error {
	port := p.SwapPort
	if port == 0 {
		port = 8089
	}
	Step("step_swap_launch", "port", itoa(port))
	cmd := exec.Command(filepath.Join(s.dir, "llama-swap.exe"), "-config", cfg, "-listen", fmt.Sprintf("127.0.0.1:%d", port))
	cmd.Dir = s.dir
	cmd.SysProcAttr = detachCmd
	if lf, err := os.OpenFile(filepath.Join(s.dir, "llama-swap.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err == nil {
		cmd.Stdout = lf
		cmd.Stderr = lf
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	_ = cmd.Process.Release()
	for i := 0; i < 16; i++ {
		time.Sleep(500 * time.Millisecond)
		if s.Health(ctx) == nil {
			return nil
		}
	}
	return fmt.Errorf("llama-swap did not become ready within 8s (see %s\\llama-swap.log)", s.dir)
}

func waitSwapDown(ctx context.Context, url string) bool {
	c := &http.Client{Timeout: 1 * time.Second}
	for i := 0; i < 50; i++ {
		r, err := c.Get(url + "/health")
		if err != nil || r.StatusCode != 200 {
			return true
		}
		r.Body.Close()
		select {
		case <-ctx.Done():
			return false
		case <-time.After(200 * time.Millisecond):
		}
	}
	return false
}

func killSwapOnPort(port int) {
	if port == 0 {
		port = 8089
	}
	_ = killOnPort(port)
}

func cfgFingerprint(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

func cfgStateFile(dir string) string { return filepath.Join(dir, "llama-swap.state.json") }

type cfgState struct {
	ConfigHash string `json:"config_hash"`
	At         string `json:"at"`
}

func sameCfg(dir, fp string) bool {
	b, err := os.ReadFile(cfgStateFile(dir))
	if err != nil {
		return false // 没记过：宁可多重启一次，也不让旧配置继续跑
	}
	var st cfgState
	if json.Unmarshal(b, &st) != nil {
		return false
	}
	return st.ConfigHash == fp
}

func writeCfgState(dir, fp string) error {
	b, _ := json.Marshal(cfgState{ConfigHash: fp, At: time.Now().Format("2006-01-02 15:04:05")})
	return os.WriteFile(cfgStateFile(dir), b, 0o644)
}

func (s *Swap) Health(ctx context.Context) error {
	c := &http.Client{Timeout: 2 * time.Second}
	r, err := c.Get(s.url + "/health")
	if err != nil {
		return err
	}
	defer r.Body.Close()
	if r.StatusCode != 200 {
		return fmt.Errorf("health HTTP %d", r.StatusCode)
	}
	return nil
}

type swapModel struct {
	ID            string `json:"id"`
	Object        string `json:"object"`
	OwnedBy       string `json:"owned_by"`
	Name          string `json:"name"`
	ContextLength int    `json:"context_length"`
	Status        struct {
		Value string `json:"value"` // "loaded" = 已加载在跑
	} `json:"status"`
}

func (s *Swap) Models(ctx context.Context) ([]Model, error) {
	c := &http.Client{Timeout: 5 * time.Second}
	r, err := c.Get(s.url + "/v1/models")
	if err != nil {
		return nil, err
	}
	defer r.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	var v struct {
		Data []swapModel `json:"data"`
	}
	if err := json.Unmarshal(b, &v); err != nil {
		return nil, fmt.Errorf("cannot parse /v1/models: %w", err)
	}
	var out []Model
	for _, m := range v.Data {
		name := m.Name
		if name == "" {
			name = m.ID
		}
		out = append(out, Model{Key: m.ID, Name: name, Engine: "llama-swap", Ctx: m.ContextLength, Running: m.Status.Value == "loaded", Source: "engine:llama-swap"})
	}
	return out, nil
}

func (s *Swap) Running(ctx context.Context) []string {
	c := &http.Client{Timeout: 3 * time.Second}
	r, err := c.Get(s.url + "/running")
	if err != nil {
		return nil
	}
	defer r.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	var v struct {
		Running []struct {
			Model string `json:"model"`
			State string `json:"state"`
		} `json:"running"`
	}
	_ = json.Unmarshal(b, &v)
	var ids []string
	for _, x := range v.Running {
		ids = append(ids, x.Model)
	}
	return ids
}

func (s *Swap) Start(ctx context.Context, id string, embed bool) error {
	var body []byte
	path := "/v1/chat/completions"
	if embed {
		path = "/v1/embeddings"
		body = []byte(`{"model":"` + id + `","input":"warmup"}`)
	} else {
		body = []byte(`{"model":"` + id + `","messages":[{"role":"user","content":"ok"}],"max_tokens":1}`)
	}
	c := &http.Client{Timeout: 300 * time.Second} // 大模型首次加载要时间
	r, err := c.Do(mustReq(ctx, "POST", s.url+path, body))
	if err != nil {
		return err
	}
	defer r.Body.Close()
	io.Copy(io.Discard, r.Body)
	if r.StatusCode >= 300 {
		return fmt.Errorf("loading %s returned HTTP %d", id, r.StatusCode)
	}
	return nil
}

func (s *Swap) Stop(ctx context.Context) error {
	return s.stopAt(ctx, "/api/models/unload")
}

func (s *Swap) StopOne(ctx context.Context, id string) error {
	return s.stopAt(ctx, "/api/models/unload/"+id)
}

func (s *Swap) stopAt(ctx context.Context, path string) error {
	req := mustReq(ctx, "POST", s.url+path, nil)
	c := &http.Client{Timeout: 30 * time.Second}
	r, err := c.Do(req)
	if err != nil {
		return err
	}
	defer r.Body.Close()
	io.Copy(io.Discard, r.Body)
	if r.StatusCode >= 300 {
		return fmt.Errorf("unload returned HTTP %d", r.StatusCode)
	}
	return nil
}

func mustReq(ctx context.Context, method, url string, body []byte) *http.Request {
	var r io.Reader
	if body != nil {
		r = bytes.NewReader(body)
	}
	req, _ := http.NewRequestWithContext(ctx, method, url, r)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req
}
