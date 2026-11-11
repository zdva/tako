// Command tako is the desktop terminal for a fleet of servers. This file only
// assembles the application and the window; the work happens in internal/.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/wei-dong/tako/internal/app"
	"github.com/wei-dong/tako/internal/store"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "0.2.0-m2"

const (
	defaultWidth  = 1280
	defaultHeight = 800
	minWidth      = 720
	minHeight     = 480
	// titleBarHeight is the height the window controls are centered in; it
	// matches the toolbar.
	titleBarHeight = 44
)

func main() {
	showVersion := flag.Bool("version", false, "print the version and exit")
	exitAfter := flag.Duration("exit-after", 0, "quit after this long (for measurements); 0 keeps running")
	flag.Parse()
	if *showVersion {
		fmt.Println("tako", version)
		return
	}
	if err := run(*exitAfter); err != nil {
		fmt.Fprintln(os.Stderr, "tako:", err)
		os.Exit(1)
	}
}

func run(exitAfter time.Duration) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	dirs, err := store.DefaultDirs()
	if err != nil {
		return fmt.Errorf("finding data directories: %w", err)
	}
	a, err := app.New(ctx, app.Options{
		Dirs: dirs, Locale: locale(), Version: version, PickFile: pickFile,
		OnQuit: func() { mygo.App.Quit() },
	})
	if err != nil {
		return fmt.Errorf("starting: %w", err)
	}
	defer a.Close()

	started := time.Now()
	mygo.App.WhenReady(func() {
		slog.Info("runtime ready", "since_start", time.Since(started))
		win := mygo.NewWindow(mygo.WindowOptions{
			Title: "Tako", Width: defaultWidth, Height: defaultHeight,
			MinWidth: minWidth, MinHeight: minHeight,
			// The page owns the whole window: the brand row and the toolbar
			// are its title bar, the window controls sit over it.
			TitleBarStyle: mygo.TitleBarHidden, TitleBarHeight: titleBarHeight,
			Content: ui.View(a.UI.Build),
		})
		a.SetWake(win.Invalidate)
		// Restore after the window drew its first frame, on the UI
		// goroutine: terminals created before that render black (seen with
		// Mesa's software GL under Xvfb).
		var once sync.Once
		win.OnReadyToShow(func() {
			once.Do(func() {
				a.UI.Post(func() {
					a.Restore()
					slog.Info("session restored", "since_start", time.Since(started))
				})
			})
		})
		slog.Info("window created", "since_start", time.Since(started))
		if exitAfter > 0 {
			time.AfterFunc(exitAfter, func() { mygo.App.Quit() })
		}
	})
	if err := mygo.App.Run(); err != nil {
		slog.Error("the app stopped", "err", err)
		return fmt.Errorf("running: %w", err)
	}
	return nil
}

// locale is the system language, from the usual environment variables.
func locale() string {
	for _, k := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if v := os.Getenv(k); v != "" {
			return v
		}
	}
	return mygo.App.Locale()
}

func pickFile(title string) (string, error) {
	paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Title: title, ShowHiddenFiles: true})
	if err != nil || len(paths) == 0 {
		return "", err //nolint:wrapcheck // a cancelled dialog is not an error to explain
	}
	return paths[0], nil
}
