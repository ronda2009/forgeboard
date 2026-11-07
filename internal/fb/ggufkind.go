package fb

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/abrander/gguf"
)

type kindEntry struct {
	kind string
	size int64
	mod  time.Time
}

var kindCache = map[string]kindEntry{}

func KindFromGGUF(path string) (kind string, ok bool) {
	st, err := os.Stat(path)
	if err != nil {
		return "", false
	}
	if e, hit := kindCache[path]; hit && e.size == st.Size() && e.mod.Equal(st.ModTime()) {
		return e.kind, true
	}
	r, err := gguf.OpenFile(path)
	if err != nil {
		return "", false
	}
	defer r.Close()
	m := r.Metadata
	for k, v := range m {
		low := strings.ToLower(k)
		if strings.HasSuffix(low, ".pooling_type") && poolingActive(v) {
			return cacheKind(path, st, "embed"), true
		}
		if strings.Contains(low, ".vision.") || strings.HasPrefix(low, "clip.") {
			return cacheKind(path, st, "vision"), true
		}
	}
	if hasMMProjSibling(path) {
		return cacheKind(path, st, "vision"), true
	}
	return cacheKind(path, st, "text"), true
}

func cacheKind(path string, st os.FileInfo, kind string) string {
	kindCache[path] = kindEntry{kind: kind, size: st.Size(), mod: st.ModTime()}
	return kind
}

func poolingActive(v any) bool {
	switch n := v.(type) {
	case uint8:
		return n != 0
	case uint16:
		return n != 0
	case uint32:
		return n != 0
	case uint64:
		return n != 0
	case int:
		return n != 0
	case int32:
		return n != 0
	case int64:
		return n != 0
	case string:
		s := strings.ToLower(strings.TrimSpace(n))
		return s != "" && s != "none"
	}
	return false
}

func hasMMProjSibling(path string) bool {
	dir, base := filepath.Dir(path), filepath.Base(path)
	stem := strings.ToLower(strings.TrimSuffix(base, filepath.Ext(base)))
	if i := strings.Index(stem, "."); i > 0 {
		stem = stem[:i]
	}
	if stem == "" {
		return false
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range ents {
		if e.IsDir() {
			continue
		}
		n := strings.ToLower(e.Name())
		if strings.Contains(n, "mmproj") && strings.HasPrefix(n, stem) && strings.EqualFold(filepath.Ext(n), ".gguf") {
			return true
		}
	}
	return false
}

var reModelPath = regexp.MustCompile(`[A-Za-z]:\\\\[^"]+?\.gguf|\B/[^\s"]+\.gguf`)

func ggufDirs(p Paths) []string {
	dirs := []string{}
	seen := map[string]bool{}
	add := func(d string) {
		if d == "" {
			return
		}
		d = filepath.Clean(d)
		if seen[d] {
			return
		}
		seen[d] = true
		dirs = append(dirs, d)
	}
	add(p.MODELS)
	for _, f := range tierFiles(p) {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		for _, raw := range reModelPath.FindAllString(string(b), -1) {
			add(filepath.Dir(strings.ReplaceAll(raw, `\\`, `\`)))
		}
	}
	return dirs
}

func ggufLib(dirs []string) map[string]string {
	lib := map[string]string{}
	for _, root := range dirs {
		var walk func(string, int)
		walk = func(dir string, depth int) {
			if depth > 3 || len(lib) > 500 {
				return
			}
			ents, err := os.ReadDir(dir)
			if err != nil {
				return
			}
			for _, e := range ents {
				full := filepath.Join(dir, e.Name())
				if e.IsDir() {
					walk(full, depth+1)
					continue
				}
				if strings.EqualFold(filepath.Ext(e.Name()), ".gguf") {
					if strings.Contains(strings.ToLower(e.Name()), "mmproj") {
						continue // 投影文件不是模型本体（它只用来判「旁边有没有视觉塔」）
					}
					lib[normName(e.Name())] = full
				}
			}
		}
		walk(root, 0)
	}
	return lib
}

func matchGGUF(lib map[string]string, names ...string) string {
	var best, bestKey string
	for _, raw := range names {
		n := normName(raw)
		if len(n) < 5 {
			continue // 太短的名字容易误配
		}
		for k, path := range lib {
			if len(k) < 5 {
				continue
			}
			if strings.Contains(n, k) || strings.Contains(k, n) {
				if len(k) > len(bestKey) {
					bestKey, best = k, path
				}
			}
		}
		if best != "" {
			return best
		}
	}
	return best
}

var nameCache = map[string]nameEntry{}

type nameEntry struct {
	name string
	size int64
	mod  time.Time
}

func NameFromGGUF(path string) string {
	st, err := os.Stat(path)
	if err != nil {
		return ""
	}
	if e, hit := nameCache[path]; hit && e.size == st.Size() && e.mod.Equal(st.ModTime()) {
		return e.name
	}
	name := ""
	if r, err := gguf.OpenFile(path); err == nil {
		if v, ok := r.Metadata["general.name"]; ok {
			name = strings.TrimSpace(fmt.Sprint(v))
		}
		r.Close()
	}
	nameCache[path] = nameEntry{name: name, size: st.Size(), mod: st.ModTime()}
	return name
}

//
//
func TierModels(p Paths) (names map[string]string, sizes map[string]int) {
	names, sizes = map[string]string{}, map[string]int{}
	for _, f := range tierFiles(p) {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		t := string(b)
		tier := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(f), "strata-"), ".json")
		mn := reModelName.FindStringSubmatch(t)
		var first string
		total := 0
		seen := map[string]bool{}
		for _, raw := range reModelPath.FindAllString(t, -1) {
			path := strings.ReplaceAll(raw, `\\`, `\`)
			if seen[path] {
				continue
			}
			seen[path] = true
			if strings.Contains(strings.ToLower(filepath.Base(path)), "mmproj") {
				continue
			}
			if first == "" {
				first = path
			}
			if st, err := os.Stat(path); err == nil {
				total += int(st.Size() / (1024 * 1024))
			}
		}
		if len(mn) > 1 && first != "" {
			names[mn[1]] = first
		}
		if total > 0 {
			sizes[tier] = total
			if len(mn) > 1 {
				sizes[mn[1]] = total // 挂载清单里的 id 就是 model_name，排序按这个键查
			}
		}
	}
	return names, sizes
}

var reModelName = regexp.MustCompile(`"model_name"\s*:\s*"([^"]+)"`)

func EnrichKinds(p Paths, c *OCConfig) {
	if c == nil {
		return
	}
	lib := ggufLib(ggufDirs(p))
	tn, _ := TierModels(p)
	if len(lib) == 0 && len(tn) == 0 {
		return
	}
	for i := range c.Providers {
		for j := range c.Providers[i].Models {
			m := &c.Providers[i].Models[j]
			path := matchGGUF(lib, m.ID, m.Name)
			if path == "" {
				continue
			}
			kind, ok := KindFromGGUF(path)
			if !ok {
				continue
			}
			if kind == "text" && m.Vision {
				continue
			}
			m.Kind, m.Embed, m.Vision = kind, kind == "embed", kind == "vision"
			m.KindFile = kind
			if m.GgufName == "" {
				m.GgufName = NameFromGGUF(path)
			}
		}
	}
	if len(tn) == 0 {
		tn, _ = TierModels(p)
	}
	for i := range c.Providers {
		for j := range c.Providers[i].Models {
			m := &c.Providers[i].Models[j]
			if m.GgufName != "" {
				continue
			}
			if path, ok := tn[m.ID]; ok {
				m.GgufName = NameFromGGUF(path)
				if k, okk := KindFromGGUF(path); okk {
					m.KindFile = k
				}
			}
		}
	}
}

var reQuant = regexp.MustCompile(`(iq[0-9][a-z0-9_]*|dq[0-9][a-z0-9_]*|q[0-9][a-z0-9_]*)$`)

func normName(s string) string {
	s = strings.ToLower(strings.TrimSuffix(filepath.Base(s), filepath.Ext(s)))
	s = keyRe.ReplaceAllString(s, "")
	for strings.HasPrefix(s, "mmproj") {
		s = strings.TrimPrefix(s, "mmproj")
	}
	return reQuant.ReplaceAllString(s, "")
}
