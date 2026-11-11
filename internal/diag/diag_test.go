package diag_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wei-dong/tako/internal/diag"
)

func TestScrub(t *testing.T) {
	t.Parallel()
	names := []string{"web-01", "db.prod.example.com"}
	tests := []struct{ in, want, absent string }{
		{`connecting to web-01 as deploy@10.0.1.11:22`, "<host>", "10.0.1.11"},
		{`dial db.prod.example.com: refused`, "dial <host>: refused", "example"},
		{`{"msg":"x","password":"hunter2","path":"/home/wei/.ssh/id_ed25519"}`, "[redacted]", "hunter2"},
		{`opened /home/wei/.config/tako/hosts.json`, "~/.config/tako/hosts.json", "/home/wei"},
		{`token=abcdef0123456789abcdef0123456789abcdef`, "[redacted]", "abcdef0123456789"},
		{`fe80::1ff:fe23:4567:890a is up`, "<ip>", "fe80"},
		{`mail me at someone@example.org`, "<user@host>", "someone"},
		{`version 1.2.3.4.5 is not an address`, "1.2.3.4.5", ""},
	}
	for _, tt := range tests {
		got := diag.Scrub(tt.in, names, "/home/wei")
		if !strings.Contains(got, tt.want) || (tt.absent != "" && strings.Contains(got, tt.absent)) {
			t.Errorf("Scrub(%q) = %q (want %q, without %q)", tt.in, got, tt.want, tt.absent)
		}
	}
	if got := diag.Scrub("999.1.1.1", nil, ""); got != "999.1.1.1" {
		t.Errorf("not an address, changed: %q", got)
	}
}

func TestBuildHasHeaderLogAndScrubbedCrash(t *testing.T) {
	t.Parallel()
	out := diag.Build(diag.Info{
		Version: "0.4.0", Locale: "zh_CN.UTF-8", Hosts: 3, Blocks: 2, Started: time.Now().Add(-90 * time.Second),
		Names: []string{"web-01"}, Home: "/home/wei",
	}, []string{`{"msg":"connected to web-01"}`}, "panic: boom\n\tat /home/wei/go/src/x.go:1\n")
	for _, want := range []string{"Tako 0.4.0", "Hosts 3, blocks 2", "connected to <host>", "panic: boom", "~/go/src/x.go", "running 1m"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "web-01") || strings.Contains(out, "/home/wei") {
		t.Errorf("not scrubbed:\n%s", out)
	}
}

func TestTail(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "log")
	var b strings.Builder
	for i := range 200 {
		b.WriteString(strings.Repeat("x", 20) + string(rune('a'+i%26)) + "\n")
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	got := diag.Tail(path, 5, 1<<20)
	if len(got) != 5 || !strings.HasSuffix(got[4], "r") { // the last line is number 199: 'a' + 199%26
		t.Fatalf("tail: %q", got)
	}
	small := diag.Tail(path, 1000, 100) // reads about four lines; the cut first line is dropped
	if len(small) < 2 || len(small) > 5 {
		t.Errorf("small read: %d lines", len(small))
	}
	if diag.Tail(filepath.Join(t.TempDir(), "none"), 5, 100) != nil {
		t.Error("a missing file has lines")
	}
}

// TestCrashReportSurvivesAPanicInAnotherGoroutine runs a child process that
// installs the handler and panics on a goroutine; the report must be there,
// and the next Install must find it.
func TestCrashReportSurvivesAPanicInAnotherGoroutine(t *testing.T) {
	if dir := os.Getenv("TAKO_DIAG_CHILD"); dir != "" {
		if _, err := diag.Install(dir); err != nil {
			os.Exit(3)
		}
		done := make(chan struct{})
		go func() { panic("tako test boom") }()
		<-done
		return
	}
	dir := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=TestCrashReportSurvivesAPanicInAnotherGoroutine")
	cmd.Env = append(os.Environ(), "TAKO_DIAG_CHILD="+dir)
	if err := cmd.Run(); err == nil {
		t.Fatal("the child did not crash")
	}
	c, err := diag.Install(dir)
	if err != nil {
		t.Fatal(err)
	}
	if c.Previous == "" {
		t.Fatal("the crash of the last run was not found")
	}
	text, err := diag.ReadReport(c.Previous, 4096)
	if err != nil || !strings.Contains(text, "tako test boom") {
		t.Fatalf("report: %q, %v", text, err)
	}
	if diag.Stamp(c.Previous).IsZero() {
		t.Error("the report's time was not read from its name")
	}
	if len(c.Reports()) != 1 {
		t.Errorf("reports: %v", c.Reports())
	}
	c.Dismiss()
	if c.Previous != "" {
		t.Error("Dismiss kept it")
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	// A clean run leaves nothing pending, so the next start reports nothing.
	again, err := diag.Install(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	if again.Previous != "" {
		t.Errorf("a clean exit was reported as a crash: %s", again.Previous)
	}
}

func TestOldReportsArePruned(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for i := range 9 {
		name := filepath.Join(dir, "crash-2026010"+string(rune('1'+i))+"-120000.log")
		if err := os.WriteFile(name, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	c, err := diag.Install(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if got := len(c.Reports()); got != diag.KeepReports {
		t.Errorf("kept %d reports, want %d", got, diag.KeepReports)
	}
}
