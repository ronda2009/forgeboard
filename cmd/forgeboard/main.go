//
package main

import (
	"context"
	"embed"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"strings"
	"syscall"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"

	"github.com/ronda2009/forgeboard/internal/app"
	"github.com/ronda2009/forgeboard/internal/cli"
	"github.com/ronda2009/forgeboard/internal/fb"
)

//
//go:embed all:frontend/dist
var rawAssets embed.FS

//
//go:embed assets/tray.ico
var trayIcon []byte

var assets fs.FS = func() fs.FS {
	f, err := fs.Sub(rawAssets, "frontend/dist")
	if err != nil {
		panic(err)
	}
	return f
}()

func init() {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	kernel32.NewProc("SetConsoleOutputCP").Call(65001)
}

func looksLikeSubcommand(word string) bool {
	if word == "" || strings.HasPrefix(word, "-") {
		return false
	}
	if strings.ContainsAny(word, `\/.")`) || strings.Contains(word, ":") {
		return false
	}
	for _, r := range word {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return false
		}
	}
	return true
}

func main() {
	argv := os.Args[1:]
	if len(argv) > 0 && cli.IsCommand(argv[0]) {
		if err := cli.Run(context.Background(), argv); err != nil {
			fmt.Fprintln(os.Stderr, "ERROR:", err)
			os.Exit(1)
		}
		return
	}
	if len(argv) > 0 && looksLikeSubcommand(argv[0]) {
		fmt.Printf("no such command: %s\n\n", argv[0])
		cli.Usage()
		os.Exit(2)
	}

	fsFlags := flag.NewFlagSet("forgeboard", flag.ContinueOnError)
	startTab := fsFlags.String("tab", "", "start on this tab: models|sessions|system|diag|debug (for testing)")
	height := fsFlags.Int("height", 820, "window height (for testing/screenshots)")
	width := fsFlags.Int("width", 1180, "window width (for testing/screenshots)")
	label := fsFlags.String("label", "ForgeBoard", "window title and tray tooltip")
	if err := fsFlags.Parse(argv); err != nil {
		fmt.Println("bad arguments:", err)
		os.Exit(2)
	}

	app.SetTrayIcon(trayIcon)

	a := app.New(app.Deps{StartTab: *startTab, Version: cli.Version})

	title := *label
	if title == "ForgeBoard" {
		title = app.WindowTitleFor(fb.LoadPaths().Lang, cli.Version)
	} else {
		title = title + " " + cli.Version
	}
	app.SetTrayTitle(title)

	if err := wails.Run(&options.App{
		Title:     title,
		Width:     *width,
		Height:    *height,
		MinWidth:  900,
		MinHeight: 620,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 17, G: 19, B: 24, A: 1},
		Windows: &windows.Options{
			Theme: windows.Dark,
			CustomTheme: &windows.ThemeSettings{
				DarkModeTitleBar:           windows.RGB(28, 32, 39),
				DarkModeTitleBarInactive:   windows.RGB(35, 40, 49),
				DarkModeTitleText:          windows.RGB(230, 233, 239),
				DarkModeTitleTextInactive:  windows.RGB(152, 162, 179),
				DarkModeBorder:             windows.RGB(46, 52, 64),
				DarkModeBorderInactive:     windows.RGB(40, 45, 55),
				LightModeTitleBar:          windows.RGB(28, 32, 39),
				LightModeTitleBarInactive:  windows.RGB(35, 40, 49),
				LightModeTitleText:         windows.RGB(230, 233, 239),
				LightModeTitleTextInactive: windows.RGB(152, 162, 179),
				LightModeBorder:            windows.RGB(46, 52, 64),
				LightModeBorderInactive:    windows.RGB(40, 45, 55),
			},
		},
		OnBeforeClose: func(ctx context.Context) (prevent bool) {
			if app.IsQuitting() || !a.TrayReady() || !a.TrayOnClose() {
				return false
			}
			a.HideToTray()
			return true
		},
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId: "forgeboard",
			OnSecondInstanceLaunch: func(options.SecondInstanceData) {
				a.ShowMainWindow()
			},
		},
		OnStartup:  a.Startup,
		OnShutdown: func(context.Context) {},
		Bind:       []interface{}{a},
	}); err != nil {
		fmt.Println("GUI failed:", err)
		os.Exit(1)
	}
}
