package fb

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

type VersionRow struct {
	What     string `json:"what"`   // openclaw | strata | llama.cpp | llama-swap
	Local    string `json:"local"`
	Latest   string `json:"latest"`
	Date     string `json:"date"`
	Note     string `json:"note"`
	Update   string `json:"update"` // up-to-date | behind | unknown
	Upstream string `json:"upstream"`
}

func localOpenclaw() string {
	home, _ := os.UserHomeDir()
	cands := []string{
		filepath.Join(home, "AppData", "Roaming", "npm", "node_modules", "openclaw", "package.json"),
		"/usr/local/lib/node_modules/openclaw/package.json",
		"/usr/lib/node_modules/openclaw/package.json",
	}
	if ad := os.Getenv("APPDATA"); ad != "" {
		cands = append([]string{filepath.Join(ad, "npm", "node_modules", "openclaw", "package.json")}, cands...)
	}
	for _, c := range cands {
		if v := readVersionFile(c); v != "" {
			return v
		}
	}
	return ""
}

func localStrata(p Paths) string {
	var cands []string
	if p.TIER_DIR != "" {
		cands = append(cands, filepath.Join(p.TIER_DIR, "engine", "BUILD.json"))
	}
	home, _ := os.UserHomeDir()
	cands = append(cands, filepath.Join(home, ".strata", "engine", "BUILD.json"))
	for _, c := range cands {
		if v := readVersionFile(c); v != "" {
			return v
		}
	}
	return ""
}

func readVersionFile(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var j struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(b, &j); err != nil {
		return ""
	}
	return strings.TrimSpace(j.Version)
}

var (
	reBuild  = regexp.MustCompile(`(?i)\bbuild[\s:=#-]*(\d+)`)
	reBTag   = regexp.MustCompile(`(?i)\b(b\d{3,})\b`)
	reVTag   = regexp.MustCompile(`(?i)\bv?(\d{2,})(?:\.\d+)*\b`)
	runVerMu sync.Mutex
)

func exeVersion(ctx context.Context, exe string) string {
	if exe == "" {
		return ""
	}
	if _, err := os.Stat(exe); err != nil {
		return ""
	}
	runVerMu.Lock()
	defer runVerMu.Unlock()
	cctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, exe, "--version")
	cmd.SysProcAttr = hideCmd
	out, err := cmd.CombinedOutput()
	if err != nil && len(out) == 0 {
		return ""
	}
	for _, line := range strings.Split(string(out), "\n") {
		if s := strings.TrimSpace(line); strings.Contains(strings.ToLower(s), "version") || reBTag.MatchString(s) {
			return s
		}
	}
	return ""
}

func localLlamaBuild(ctx context.Context, p Paths) string {
	out := exeVersion(ctx, p.LLAMA_EXE)
	if out == "" {
		return ""
	}
	if mm := reBTag.FindStringSubmatch(out); len(mm) > 1 {
		return strings.ToLower(mm[1])
	}
	if mm := reBuild.FindStringSubmatch(out); len(mm) > 1 {
		return "b" + mm[1]
	}
	return ""
}

func localSwapVer(ctx context.Context, p Paths) string {
	out := exeVersion(ctx, filepath.Join(CfgDir(), "llama-swap.exe"))
	if out == "" {
		return ""
	}
	if mm := reVTag.FindStringSubmatch(out); len(mm) > 1 {
		return mm[1]
	}
	return ""
}

func ghLatest(ctx context.Context, repo string) (latest, date, note string) {
	cctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodGet, "https://api.github.com/repos/"+repo+"/releases?per_page=5", nil)
	if err != nil {
		return "", "", err.Error()
	}
	req.Header.Set("User-Agent", "forgeboard/2.0")
	req.Header.Set("Accept", "application/vnd.github+json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", "", err.Error()
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", "", fmt.Sprintf("HTTP %d", res.StatusCode)
	}
	var list []struct {
		TagName    string `json:"tag_name"`
		Published  string `json:"published_at"`
		Prerelease bool   `json:"prerelease"`
		Draft      bool   `json:"draft"`
	}
	if err := json.NewDecoder(res.Body).Decode(&list); err != nil {
		return "", "", "cannot parse release list"
	}
	for _, x := range list {
		if x.TagName == "" || x.Draft {
			continue
		}
		tag := strings.TrimPrefix(x.TagName, "v")
		d := x.Published
		if len(d) >= 10 {
			d = d[:10]
		}
		if x.Prerelease {
			return tag, d, "prerelease"
		}
		return tag, d, ""
	}
	return "", "", "no release"
}

func verNums(s string) []int {
	var out []int
	for _, m := range regexp.MustCompile(`\d+`).FindAllString(s, -1) {
		n, _ := strconv.Atoi(m)
		out = append(out, n)
	}
	return out
}

func cmpVer(a, b string) int {
	na, nb := verNums(a), verNums(b)
	if len(na) == 0 || len(nb) == 0 {
		return -99
	}
	for i := 0; i < len(na) || i < len(nb); i++ {
		x, y := 0, 0
		if i < len(na) {
			x = na[i]
		}
		if i < len(nb) {
			y = nb[i]
		}
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	return 0
}

func Versions(ctx context.Context, p Paths, self string) []VersionRow {
	specs := []struct{ what, repo, url string }{
		{"openclaw", "openclaw/openclaw", "https://github.com/openclaw/openclaw/releases"},
		{"strata", "Niko1221/Strata", "https://github.com/Niko1221/Strata/releases"},
		{"llama.cpp", "ggml-org/llama.cpp", "https://github.com/ggml-org/llama.cpp/releases"},
		{"llama-swap", "mostlygeek/llama-swap", "https://github.com/mostlygeek/llama-swap/releases"},
		{"forgeboard", "ronda2009/forgeboard", "https://github.com/ronda2009/forgeboard/releases"},
	}
	type got struct{ latest, date, note string }
	res := make([]got, len(specs))
	var ocLocal, stLocal, lmLocal, swLocal string
	var wg sync.WaitGroup
	for i := range specs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			res[i].latest, res[i].date, res[i].note = ghLatest(ctx, specs[i].repo)
		}(i)
	}
	wg.Add(4)
	go func() { defer wg.Done(); ocLocal = localOpenclaw() }()
	go func() { defer wg.Done(); stLocal = localStrata(p) }()
	go func() { defer wg.Done(); lmLocal = localLlamaBuild(ctx, p) }()
	go func() { defer wg.Done(); swLocal = localSwapVer(ctx, p) }()
	wg.Wait()

	localOf := map[string]string{"openclaw": ocLocal, "strata": stLocal, "llama.cpp": lmLocal, "llama-swap": swLocal, "forgeboard": self}
	var out []VersionRow
	for i, s := range specs {
		r := VersionRow{What: s.what, Local: localOf[s.what], Latest: res[i].latest, Date: res[i].date, Note: res[i].note, Upstream: s.url}
		switch {
		case r.Local == "" || r.Latest == "":
			r.Update = "unknown"
		default:
			switch cmpVer(r.Local, r.Latest) {
			case 0:
				r.Update = "up-to-date"
			case -1:
				r.Update = "behind"
			case 1:
				r.Update = "ahead"
			default:
				r.Update = "unknown"
			}
		}
		if s.what == "forgeboard" && strings.Contains(r.Local, "-dev") {
			r.Update = "unknown"
			r.Note = "note-ver-dev"
		}
		out = append(out, r)
	}
	return out
}
