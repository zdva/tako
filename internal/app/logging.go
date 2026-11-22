package app

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/zdva/tako/internal/store"
)

const redacted = "[redacted]"

// sensitiveKey reports whether a log attribute key may carry a secret.
func sensitiveKey(k string) bool {
	k = strings.ToLower(k)
	for _, frag := range []string{"pass", "secret", "token", "key", "cred", "auth"} {
		if strings.Contains(k, frag) {
			return true
		}
	}
	return false
}

// redact hides the values of sensitive attributes, whatever the caller meant.
func redact(_ []string, a slog.Attr) slog.Attr {
	if sensitiveKey(a.Key) && a.Value.Kind() != slog.KindGroup {
		// Counts and flags are harmless and useful.
		switch a.Value.Kind() {
		case slog.KindBool, slog.KindInt64, slog.KindUint64, slog.KindDuration, slog.KindTime:
			return a
		default:
			return slog.String(a.Key, redacted)
		}
	}
	return a
}

// NewLogger returns a JSON logger on f that redacts sensitive attributes.
func NewLogger(f *os.File) *slog.Logger {
	return slog.New(slog.NewJSONHandler(f, &slog.HandlerOptions{Level: slog.LevelInfo, ReplaceAttr: redact}))
}

// setupLogging opens the log file, installs the default logger and returns
// the function that closes the file.
func setupLogging(d store.Dirs) (func(), error) {
	if err := os.MkdirAll(d.Logs(), 0o700); err != nil {
		return nil, fmt.Errorf("creating the log directory: %w", err)
	}
	path := filepath.Join(d.Logs(), "tako.log")
	if fi, err := os.Stat(path); err == nil && fi.Size() > 4<<20 {
		_ = os.Rename(path, path+".1")
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, fmt.Errorf("opening the log file: %w", err)
	}
	prev := slog.Default()
	slog.SetDefault(NewLogger(f))
	return func() {
		slog.SetDefault(prev)
		_ = f.Close()
	}, nil
}
