package app

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func TestRedact_HidesSensitiveValues(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{ReplaceAttr: redact}))
	log.Info("event",
		"password", "hunter2", "passphrase", "s3cr3t", "api_token", "t0k",
		"ssh_key", "-----BEGIN", "secretRef", "host/h_1/password", "auth", "pw",
		"host", "web-01", "attempt", 3, "key_count", 2, "elapsed", time.Second,
		slog.Group("g", slog.String("Password", "inner")),
	)
	out := buf.String()
	for _, leaked := range []string{"hunter2", "s3cr3t", "t0k", "-----BEGIN", "host/h_1/password", "inner"} {
		if strings.Contains(out, leaked) {
			t.Errorf("log leaks %q: %s", leaked, out)
		}
	}
	var rec map[string]any
	if err := json.Unmarshal(buf.Bytes(), &rec); err != nil {
		t.Fatal(err)
	}
	if rec["host"] != "web-01" || rec["attempt"] != float64(3) {
		t.Errorf("harmless attributes were altered: %v", rec)
	}
	if rec["key_count"] != float64(2) {
		t.Errorf("a count under a sensitive-looking key should stay: %v", rec)
	}
}
