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
	"github.com/egoist/mygo/plugins/updater"
	"github.com/egoist/mygo/plugins/updater/native"
	"github.com/egoist/mygo/ui"

	"github.com/zdva/tako/internal/app"
	"github.com/zdva/tako/internal/diag"
	"github.com/zdva/tako/internal/store"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "0.4.0"

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
	doMigrate := flag.Bool("migrate-keychain", false, "copy passwords out of the system credential store into Tako's encrypted file, then delete them there; may ask the system to allow access")
	flag.Parse()
	if *showVersion {
		fmt.Println("tako", version)
		return
	}
	if *doMigrate {
		if err := runMigrateCommand(); err != nil {
			fmt.Fprintln(os.Stderr, "tako:", err)
			os.Exit(1)
		}
		return
	}
	if err := run(*exitAfter); err != nil {
		fmt.Fprintln(os.Stderr, "tako:", err)
		os.Exit(1)
	}
}

func runMigrateCommand() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	return migrateFromSystem(ctx)
}

func run(exitAfter time.Duration) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	dirs, err := store.DefaultDirs()
	if err != nil {
		return fmt.Errorf("finding data directories: %w", err)
	}
	// A crash on any goroutine leaves a report next to the logs; a clean
	// exit removes the empty file.
	crashes, err := diag.Install(dirs.Logs())
	if err != nil {
		slog.Warn("crash reports are off", "err", err)
	}
	defer func() { _ = crashes.Close() }()
	a, err := app.New(ctx, app.Options{
		Dirs: dirs, Locale: locale(), Version: version, PickFile: pickFile, PickSave: pickSave,
		OnQuit: func() { mygo.App.Quit() }, Crashes: crashes,
		CheckUpdates: updater.CheckForUpdates, UpdateAvailable: updater.AvailableVersion,
	})
	if err != nil {
		return fmt.Errorf("starting: %w", err)
	}
	defer a.Close()
	// Signed updates from GitHub Releases (mygo.json "updates"). The window
	// speaks the interface language; development builds never check.
	mygo.Use(native.New(updater.Options{Language: string(a.Settings.Values().ResolveLanguage(locale()))}))

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

func pickSave(title, name string) (string, error) {
	path, err := mygo.Dialog.Save(mygo.SaveDialogOptions{Title: title, DefaultPath: name, CreateDirectories: true})
	return path, err //nolint:wrapcheck // a cancelled dialog is not an error to explain
}
