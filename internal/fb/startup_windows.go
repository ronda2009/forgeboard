package fb

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/windows/registry"
)

type StartupItem struct {
	Name      string `json:"name"`
	Source    string `json:"source"` // hkcu-run | hklm-run | hklm-run32 | folder-user | folder-common
	Command   string `json:"command"`
	Enabled   bool   `json:"enabled"`
	Editable  bool   `json:"editable"` // 这条改得动吗（HKLM/公共文件夹要管理员）
	Deletable bool   `json:"deletable"`
	Note      string `json:"note"`
}

const (
	suRunPath   = `SOFTWARE\Microsoft\Windows\CurrentVersion\Run`
	suAppHKCU   = `SOFTWARE\Microsoft\Windows\CurrentVersion\Explorer\StartupApproved\Run`
	suAppHKLM   = `SOFTWARE\Microsoft\Windows\CurrentVersion\Explorer\StartupApproved\Run`
	suAppFolder = `SOFTWARE\Microsoft\Windows\CurrentVersion\Explorer\StartupApproved\StartupFolder`
	suSelfName  = "ForgeBoard"
)

func suFolders() (user string, common string) {
	if a := os.Getenv("APPDATA"); a != "" {
		user = filepath.Join(a, "Microsoft", "Windows", "Start Menu", "Programs", "Startup")
	}
	if p := os.Getenv("ProgramData"); p != "" {
		common = filepath.Join(p, "Microsoft", "Windows", "Start Menu", "Programs", "Startup")
	}
	return
}

func suApproved(root registry.Key, path, name string) (present bool, disabled bool) {
	k, err := registry.OpenKey(root, path, registry.QUERY_VALUE)
	if err != nil {
		return false, false
	}
	defer k.Close()
	b, _, err := k.GetBinaryValue(name)
	if err != nil || len(b) < 1 {
		return false, false
	}
	return true, b[0] == 0x03
}

func suFlag(disable bool) []byte {
	b := make([]byte, 12)
	if disable {
		b[0] = 0x03
		t := uint64(time.Now().UTC().UnixNano()/100 + 116444736000000000) // Windows FILETIME
		for i := 0; i < 8; i++ {
			b[4+i] = byte(t >> (8 * i))
		}
		return b
	}
	b[0] = 0x02
	return b
}

func suReadRun(root registry.Key, approved, source string, access uint32, editable bool, out *[]StartupItem) {
	k, err := registry.OpenKey(root, suRunPath, access|registry.QUERY_VALUE)
	if err != nil {
		return
	}
	defer k.Close()
	names, err := k.ReadValueNames(0)
	if err != nil {
		return
	}
	for _, n := range names {
		v, _, err := k.GetStringValue(n)
		if err != nil {
			continue
		}
		_, disabled := suApproved(root, approved, n)
		*out = append(*out, StartupItem{
			Name: n, Source: source, Command: v,
			Enabled: !disabled, Editable: editable, Deletable: editable,
		})
	}
}

func suReadFolder(dir, source string, editable bool, out *[]StartupItem) {
	if dir == "" {
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() || strings.EqualFold(e.Name(), "desktop.ini") {
			continue
		}
		_, disabled := suApproved(registry.CURRENT_USER, suAppFolder, e.Name())
		*out = append(*out, StartupItem{
			Name: e.Name(), Source: source, Command: filepath.Join(dir, e.Name()),
			Enabled: !disabled, Editable: editable, Deletable: editable,
			Note: "note-su-kind",
		})
	}
}

func StartupList() []StartupItem {
	var out []StartupItem
	suReadRun(registry.CURRENT_USER, suAppHKCU, "hkcu-run", 0, true, &out)
	suReadRun(registry.LOCAL_MACHINE, suAppHKLM, "hklm-run", registry.WOW64_64KEY, false, &out)
	suReadRun(registry.LOCAL_MACHINE, suAppHKLM, "hklm-run32", registry.WOW64_32KEY, false, &out)
	user, common := suFolders()
	suReadFolder(user, "folder-user", true, &out)
	suReadFolder(common, "folder-common", false, &out)
	return out
}

func suTarget(source string) (registry.Key, string, bool, error) {
	switch source {
	case "hkcu-run":
		return registry.CURRENT_USER, suAppHKCU, true, nil
	case "hklm-run", "hklm-run32":
		return registry.LOCAL_MACHINE, suAppHKLM, false, nil
	case "folder-user", "folder-common":
		return registry.CURRENT_USER, suAppFolder, source == "folder-user", nil
	}
	return 0, "", false, fmt.Errorf("unknown startup source %q", source)
}

var suNeedAdmin = errors.New("this item needs the panel to run as administrator")

func StartupSwitch(source, name string, enable, dry bool) (*OpResult, error) {
	if name == "" {
		return nil, errors.New("no startup item given")
	}
	root, approved, editable, err := suTarget(source)
	if err != nil {
		return nil, err
	}
	if !editable {
		return nil, suNeedAdmin
	}
	token := "su_en"
	if !enable {
		token = "su_dis"
	}
	if dry {
		_, disabled := suApproved(root, approved, name)
		was := "yes"
		if disabled {
			was = "no"
		}
		return op(token+"_dry", true).subj("name", name, "source", source, "was", was), nil
	}
	k, _, err := registry.CreateKey(root, approved, registry.SET_VALUE)
	if err != nil {
		return nil, fmt.Errorf("cannot open the task-manager switch key: %w", err)
	}
	defer k.Close()
	if err := k.SetBinaryValue(name, suFlag(!enable)); err != nil {
		return nil, fmt.Errorf("cannot write the switch: %w", err)
	}
	_, disabled := suApproved(root, approved, name)
	if disabled == enable {
		return nil, fmt.Errorf("read-back does not match (wanted enabled=%v)", enable)
	}
	return op(token+"_done", false).subj("name", name, "source", source), nil
}

func suBackup(line string) (string, error) {
	dir := filepath.Join(CfgDir(), "startup-deleted")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	f := filepath.Join(dir, "deleted.txt")
	fh, err := os.OpenFile(f, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return "", err
	}
	defer fh.Close()
	if _, err := fh.WriteString(time.Now().Format("2006-01-02 15:04:05")+" | "+line+"\n"); err != nil {
		return "", err
	}
	return f, nil
}

func StartupDelete(source, name string, dry bool) (*OpResult, error) {
	if name == "" {
		return nil, errors.New("no startup item given")
	}
	switch source {
	case "hkcu-run":
		k, err := registry.OpenKey(registry.CURRENT_USER, suRunPath, registry.QUERY_VALUE|registry.SET_VALUE)
		if err != nil {
			return nil, err
		}
		defer k.Close()
		v, _, err := k.GetStringValue(name)
		if err != nil {
			return nil, fmt.Errorf("cannot read that item: %w", err)
		}
		if dry {
			return op("su_del_dry", true).subj("name", name, "source", source).withNote(v), nil
		}
		bak, err := suBackup("registry HKCU Run | " + name + " = " + v)
		if err != nil {
			return nil, err
		}
		if err := k.DeleteValue(name); err != nil {
			return nil, fmt.Errorf("cannot delete: %w", err)
		}
		if _, _, err := k.GetStringValue(name); err == nil {
			return nil, errors.New("still there after delete")
		}
		if ak, _, err := registry.CreateKey(registry.CURRENT_USER, suAppHKCU, registry.SET_VALUE); err == nil {
			_ = ak.DeleteValue(name)
			ak.Close()
		}
		return op("su_del_done", false).subj("name", name, "source", source, "backup", bak).withNote(v), nil
	case "folder-user":
		user, _ := suFolders()
		src := filepath.Join(user, name)
		if _, err := os.Stat(src); err != nil {
			return nil, err
		}
		if dry {
			return op("su_del_dry", true).subj("name", name, "source", source).withNote(src), nil
		}
		dir := filepath.Join(CfgDir(), "startup-deleted")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
		dst := filepath.Join(dir, fmt.Sprintf("%d-%s", time.Now().Unix(), name))
		if err := os.Rename(src, dst); err != nil {
			return nil, fmt.Errorf("cannot move it out: %w", err)
		}
		if _, err := os.Stat(src); err == nil {
			return nil, errors.New("still there after delete")
		}
		if _, err := os.Stat(dst); err != nil {
			return nil, fmt.Errorf("moved but the backup copy is missing: %w", err)
		}
		if ak, _, err := registry.CreateKey(registry.CURRENT_USER, suAppFolder, registry.SET_VALUE); err == nil {
			_ = ak.DeleteValue(name)
			ak.Close()
		}
		return op("su_del_done", false).subj("name", name, "source", source, "backup", dst).withNote(src), nil
	case "hklm-run", "hklm-run32", "folder-common":
		return nil, suNeedAdmin
	}
	return nil, fmt.Errorf("unknown startup source %q", source)
}

func SelfStartState() (on bool, entry string) {
	exe, err := os.Executable()
	if err != nil {
		return false, ""
	}
	k, err := registry.OpenKey(registry.CURRENT_USER, suRunPath, registry.QUERY_VALUE)
	if err != nil {
		return false, ""
	}
	defer k.Close()
	names, err := k.ReadValueNames(0)
	if err != nil {
		return false, ""
	}
	want := strings.ToLower(exe)
	for _, n := range names {
		v, _, err := k.GetStringValue(n)
		if err != nil {
			continue
		}
		if strings.Contains(strings.ToLower(v), want) {
			return true, n + " = " + v
		}
	}
	return false, ""
}

func SelfStart(on, dry bool) (*OpResult, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	cmd := `"` + exe + `"`
	if !on {
		k, err := registry.OpenKey(registry.CURRENT_USER, suRunPath, registry.QUERY_VALUE|registry.SET_VALUE)
		if err != nil {
			return nil, err
		}
		defer k.Close()
		cur, _, err := k.GetStringValue(suSelfName)
		if err != nil {
			return op("su_self_off_done", dry).subj("command", cmd), nil // 本来就没有：幂等，别报错
		}
		if dry {
			return op("su_self_off_dry", true).subj("command", cmd).withNote(cur), nil
		}
		if err := k.DeleteValue(suSelfName); err != nil {
			return nil, fmt.Errorf("cannot remove it: %w", err)
		}
		if _, _, err := k.GetStringValue(suSelfName); err == nil {
			return nil, errors.New("still there after delete")
		}
		return op("su_self_off_done", false).subj("command", cmd).withNote(cur), nil
	}
	if dry {
		return op("su_self_dry", true).subj("command", cmd), nil
	}
	k, _, err := registry.CreateKey(registry.CURRENT_USER, suRunPath, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return nil, err
	}
	defer k.Close()
	if err := k.SetStringValue(suSelfName, cmd); err != nil {
		return nil, fmt.Errorf("cannot write it: %w", err)
	}
	v, _, err := k.GetStringValue(suSelfName)
	if err != nil || !strings.Contains(strings.ToLower(v), strings.ToLower(exe)) {
		return nil, fmt.Errorf("read-back does not match: %s", v)
	}
	return op("su_self_done", false).subj("command", cmd).withNote(v), nil
}

func StartupFolderUser() string {
	u, _ := suFolders()
	return u
}
