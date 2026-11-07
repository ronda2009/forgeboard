package fb

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

type DetectRow struct {
	Field string `json:"field"` // 要填进 forgeboard.json 的字段名
	Value string `json:"value"`
	How   string `json:"how"` // 怎么找到的（给人看一眼就信）
}

func homeDir() string {
	h, _ := os.UserHomeDir()
	return h
}

func searchRoots() []string {
	var roots []string
	if runtime.GOOS == "windows" {
		for _, d := range []string{"D:\\", "C:\\"} {
			for _, sub := range []string{"06.AItools", "03.AImodels", "AItools", "AImodels", "Software"} {
				p := filepath.Join(d, sub)
				if _, err := os.Stat(p); err == nil {
					roots = append(roots, p)
				}
			}
		}
		if ad := os.Getenv("LOCALAPPDATA"); ad != "" {
			roots = append(roots, filepath.Join(ad, "Programs"))
		}
	} else {
		for _, p := range []string{"/usr/local", "/opt", filepath.Join(homeDir(), ".local")} {
			if _, err := os.Stat(p); err == nil {
				roots = append(roots, p)
			}
		}
	}
	if h := homeDir(); h != "" {
		roots = append(roots, h)
	}
	return roots
}

func walkLimited(root string, maxDepth int, visit func(path string, fi os.FileInfo) bool) {
	base := strings.Count(filepath.Clean(root), string(os.PathSeparator))
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if strings.HasPrefix(d.Name(), ".") || d.Name() == "node_modules" || d.Name() == "$RECYCLE.BIN" {
				return filepath.SkipDir
			}
			if strings.Count(filepath.Clean(p), string(os.PathSeparator))-base > maxDepth {
				return filepath.SkipDir
			}
			return nil
		}
		fi, err := d.Info()
		if err != nil {
			return nil
		}
		if visit(p, fi) {
			return filepath.SkipAll
		}
		return nil
	})
}

func detectLlamaExe() DetectRow {
	type hit struct {
		path string
		mod  time.Time
	}
	var hits []hit
	for _, r := range searchRoots() {
		root := r
		walkLimited(root, 4, func(p string, fi os.FileInfo) bool {
			n := strings.ToLower(fi.Name())
			if n == "llama-server.exe" || n == "llama-server" {
				hits = append(hits, hit{p, fi.ModTime()})
			}
			return false
		})
	}
	if len(hits) == 0 {
		return DetectRow{Field: "LLAMA_EXE", How: "not found (no llama-server under the usual roots)"}
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].mod.After(hits[j].mod) })
	return DetectRow{Field: "LLAMA_EXE", Value: hits[0].path, How: "newest of " + itoa(len(hits)) + " llama-server found"}
}

func detectModelsDir() DetectRow {
	counts := map[string]int{}
	for _, r := range searchRoots() {
		root := r
		walkLimited(root, 4, func(p string, fi os.FileInfo) bool {
			low := strings.ToLower(fi.Name())
			if strings.EqualFold(filepath.Ext(p), ".gguf") && !strings.Contains(low, "mmproj") && !strings.HasPrefix(low, "ggml-vocab-") { // 词表与投影文件不是模型本体
				counts[filepath.Dir(p)]++
			}
			return false
		})
	}
	best, bestN := "", 0
	for d, n := range counts {
		if n > bestN {
			best, bestN = d, n
		}
	}
	if best == "" {
		return DetectRow{Field: "MODELS", How: "not found (no .gguf under the usual roots)"}
	}
	return DetectRow{Field: "MODELS", Value: best, How: "directory holding the most .gguf (" + itoa(bestN) + ")"}
}

func detectTierDir() DetectRow {
	seen := map[string]bool{}
	var dirs []string
	for _, r := range searchRoots() {
		root := r
		walkLimited(root, 3, func(p string, fi os.FileInfo) bool {
			if strings.HasPrefix(strings.ToLower(fi.Name()), "strata-") && strings.EqualFold(filepath.Ext(p), ".json") {
				if b, err := os.ReadFile(p); err != nil || !reModelName.Match(b) {
					return false
				}
				d := filepath.Dir(p)
				if !seen[d] {
					seen[d] = true
					dirs = append(dirs, d)
				}
			}
			return false
		})
	}
	if len(dirs) == 0 {
		return DetectRow{Field: "TIER_DIR", How: "not found (no strata-*.json under the usual roots)"}
	}
	sort.Strings(dirs)
	return DetectRow{Field: "TIER_DIR", Value: dirs[0], How: "directory holding strata-*.json tier files (" + itoa(len(dirs)) + " candidates)"}
}

func detectOpenclaw() (DetectRow, DetectRow) {
	var cands []string
	if h := homeDir(); h != "" {
		cands = append(cands, filepath.Join(h, ".openclaw", "openclaw.json"))
	}
	if ad := os.Getenv("APPDATA"); ad != "" {
		cands = append(cands, filepath.Join(ad, "openclaw", "openclaw.json"))
	}
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		cands = append(cands, filepath.Join(x, "openclaw", "openclaw.json"))
	}
	for _, c := range cands {
		if _, err := os.Stat(c); err == nil {
			return DetectRow{Field: "OC_CFG", Value: c, How: "found at the standard location"},
				DetectRow{Field: "OC_HOME", Value: filepath.Dir(c), How: "the directory holding it"}
		}
	}
	return DetectRow{Field: "OC_CFG", How: "not found (looked in ~/.openclaw, %APPDATA%\\openclaw, $XDG_CONFIG_HOME/openclaw)"},
		DetectRow{Field: "OC_HOME", How: "follows OC_CFG"}
}

func detectRescue() DetectRow {
	var hits []string
	for _, r := range searchRoots() {
		root := r
		walkLimited(root, 5, func(p string, fi os.FileInfo) bool {
			if strings.EqualFold(fi.Name(), "gateway-rescue.ps1") {
				hits = append(hits, p)
			}
			return false
		})
	}
	if len(hits) == 0 {
		return DetectRow{Field: "GW_RESCUE", How: "not found (gateway-rescue.ps1 nowhere in the usual roots - rescue button will say so)"}
	}
	sort.Strings(hits)
	return DetectRow{Field: "GW_RESCUE", Value: hits[0], How: "found gateway-rescue.ps1 (" + itoa(len(hits)) + " copies, first by path)"}
}

func DetectRouter(ctx context.Context) DetectRow {
	for _, port := range []int{8080, 8081, 8090, 7860} {
		u := "http://127.0.0.1:" + itoa(port)
		if rs := AskRouter(ctx, u); rs != nil && len(rs.Tiers) > 0 {
			return DetectRow{Field: "RouterURL", Value: u, How: itoa(len(rs.Tiers)) + " tiers answered on /__status"}
		}
	}
	return DetectRow{Field: "RouterURL", How: "no router answered on the usual ports (leave as is if you run llama-swap only)"}
}

func Detect(ctx context.Context) []DetectRow {
	oc, home := detectOpenclaw()
	out := []DetectRow{
		detectModelsDir(),
		detectLlamaExe(),
		detectTierDir(),
		oc,
		home,
		detectRescue(),
		DetectRouter(ctx),
		detectPython(),
		detectServe(),
	}
	return out
}

func detectPython() DetectRow {
	if v := routerPython(tierRoot(LoadPaths())); v != "" {
		return DetectRow{Field: "PY", Value: v, How: "Strata .venv first, then PATH"}
	}
	return DetectRow{Field: "PY", How: "no python found (.venv or PATH) - leave as is"}
}

func detectServe() DetectRow {
	if d := routerScriptDir(LoadPaths()); d != "" {
		return DetectRow{Field: "SERVE", Value: d, How: "directory holding strata-router.py"}
	}
	return DetectRow{Field: "SERVE", How: "no strata-router.py under the tier dir - leave as is"}
}
