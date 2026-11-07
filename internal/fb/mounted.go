package fb

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func OCConfigPath(p Paths) string {
	if p.OC_CFG != "" {
		return p.OC_CFG
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".openclaw", "openclaw.json")
}

type MountedModel struct {
	Provider string `json:"provider"`
	BaseURL  string `json:"baseUrl"`
	Host     string `json:"host"` // local = 本机端口；remote = 远程 API
	Port     int    `json:"port,omitempty"`
	ID       string `json:"id"`
	Name     string `json:"name"`
	Ctx      int    `json:"ctx"`
	Vision   bool   `json:"vision"`
	Embed    bool   `json:"embed"`
	Tools    bool   `json:"tools"`
	Reasoner bool   `json:"reasoner"`
	Running  bool   `json:"running"`            // 它那个服务端口现在在跑且报了这个 id
	Kind     string `json:"kind"`               // 界面「模型类型」那列：embed | vision | text（+tools/reasoner 另标）
	KindFile string `json:"kindFile,omitempty"` // 类型是从模型文件元数据读出来的那份（读不到就没值；隐藏嵌入/视觉靠它）

	GgufName string `json:"ggufName,omitempty"` // 模型文件自己的名字（元数据 general.name，作者打包时写的）；空＝文件里没写
}

type OCConfig struct {
	GatewayPort int
	Providers   []OCProvider
}

type OCProvider struct {
	Name    string
	BaseURL string
	Models  []MountedModel
}

func LoadOCConfig(p Paths) (*OCConfig, error) {
	b, err := os.ReadFile(OCConfigPath(p))
	if err != nil {
		return nil, err
	}
	var j struct {
		Models struct {
			Providers map[string]struct {
				BaseURL string `json:"baseUrl"`
				Models  []struct {
					ID            string   `json:"id"`
					Name          string   `json:"name"`
					Input         []string `json:"input"`
					ContextWindow int      `json:"contextWindow"`
					Reasoning     *bool    `json:"reasoning"`
					Compat        struct {
						SupportsTools *bool `json:"supportsTools"`
					} `json:"compat"`
				} `json:"models"`
			} `json:"providers"`
		} `json:"models"`
		Gateway struct {
			Port int `json:"port"`
		} `json:"gateway"`
	}
	if err := json.Unmarshal(b, &j); err != nil {
		return nil, err
	}
	c := &OCConfig{GatewayPort: j.Gateway.Port}
	for name, pv := range j.Models.Providers {
		pr := OCProvider{Name: name, BaseURL: pv.BaseURL}
		for _, m := range pv.Models {
			if m.ID == "" {
				continue
			}
			mm := MountedModel{Provider: name, BaseURL: pv.BaseURL, ID: m.ID, Ctx: m.ContextWindow}
			mm.Name = m.ID
			if m.Name != "" {
				mm.Name = m.Name
			}
			host, port := splitURL(pv.BaseURL)
			mm.Host, mm.Port = host, port
			for _, x := range m.Input {
				if strings.EqualFold(x, "image") {
					mm.Vision = true
				}
			}
			if m.Compat.SupportsTools != nil {
				mm.Tools = *m.Compat.SupportsTools
			}
			if m.Reasoning != nil {
				mm.Reasoner = *m.Reasoning
			}
			mm.Embed = looksEmbed(name, m.ID, mm.Name)
			mm.Kind = modelKind(mm)
			pr.Models = append(pr.Models, mm)
		}
		if len(pr.Models) > 0 {
			c.Providers = append(c.Providers, pr)
		}
	}
	return c, nil
}

func looksEmbed(provider, id, name string) bool {
	s := strings.ToLower(provider + " " + id + " " + name)
	if strings.Contains(s, "embed") {
		return true
	}
	for _, pre := range []string{"bge", "mxbai", "nomic", "e5-", "gte-", "jina-embed", "snowflake-arctic"} {
		if strings.Contains(strings.ToLower(id), pre) {
			return true
		}
	}
	return false
}

func modelKind(m MountedModel) string {
	switch {
	case m.Embed:
		return "embed"
	case m.Vision:
		return "vision"
	default:
		return "text"
	}
}

func splitURL(u string) (string, int) {
	s := strings.ToLower(strings.TrimSpace(u))
	i := strings.Index(s, "://")
	if i >= 0 {
		s = s[i+3:]
	}
	j := strings.Index(s, "/")
	if j >= 0 {
		s = s[:j]
	}
	port := 0
	host := s
	if k := strings.LastIndex(s, ":"); k >= 0 {
		host = s[:k]
		for _, c := range s[k+1:] {
			if c < '0' || c > '9' {
				port = 0
				break
			}
			port = port*10 + int(c-'0')
		}
	}
	if host == "127.0.0.1" || host == "localhost" || host == "::1" {
		host = "local"
	} else if host == "" {
		host = "remote"
	}
	if host != "local" {
		return "remote", port
	}
	return "local", port
}

func MarkMountedRunning(ctx context.Context, c *OCConfig) {
	seen := map[string]map[string]map[string]any{} // baseUrl -> {id(小写): 模型对象}
	for i := range c.Providers {
		pr := &c.Providers[i]
		if pr.Host() != "local" || pr.BaseURL == "" {
			continue
		}
		data, ok := seen[pr.BaseURL]
		if !ok {
			data = fetchModels(ctx, pr.BaseURL)
			if data == nil {
				data = map[string]map[string]any{}
			}
			seen[pr.BaseURL] = data
		}
		for j := range pr.Models {
			m := &pr.Models[j]
			o, hit := data[strings.ToLower(m.ID)]
			if !hit {
				continue
			}
			m.Running = true
			if mods := modalitiesOf(o); len(mods) > 0 {
				for _, x := range mods {
					if strings.EqualFold(x, "image") {
						m.Vision = true
					}
				}
				m.Kind = modelKind(*m)
			}
		}
	}
}

func (p OCProvider) Host() string {
	h, _ := splitURL(p.BaseURL)
	return h
}

func (p OCProvider) Port() int {
	_, port := splitURL(p.BaseURL)
	return port
}

func fetchModels(ctx context.Context, baseURL string) map[string]map[string]any {
	u := strings.TrimRight(baseURL, "/")
	if !strings.HasSuffix(strings.ToLower(u), "/v1") {
		u += "/v1"
	}
	cctx, cancel := context.WithTimeout(ctx, 2500*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodGet, u+"/models", nil)
	if err != nil {
		return nil
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil
	}
	var j struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.NewDecoder(res.Body).Decode(&j); err != nil {
		return nil
	}
	out := map[string]map[string]any{}
	for _, m := range j.Data {
		if id := str(m["id"]); id != "" {
			out[strings.ToLower(id)] = m
		}
	}
	return out
}

func modalitiesOf(o map[string]any) []string {
	arch, _ := o["architecture"].(map[string]any)
	if arch == nil {
		return nil
	}
	raw, _ := arch["input_modalities"].([]any)
	var out []string
	for _, x := range raw {
		if s, ok := x.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func PortUp(port int) bool {
	c, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", itoa(port)), 300*time.Millisecond)
	if err != nil {
		return false
	}
	c.Close()
	return true
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
