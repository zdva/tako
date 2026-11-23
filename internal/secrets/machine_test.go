package secrets

import (
	"bytes"
	"runtime"
	"strings"
	"testing"
)

func TestNormalizeMachineID(t *testing.T) {
	t.Parallel()
	id, err := normalizeMachineID([]byte("  abcdef0123456789\n"))
	if err != nil || id != "abcdef0123456789" {
		t.Fatalf("id = %q, %v", id, err)
	}
	if _, err := normalizeMachineID([]byte("has space")); err == nil {
		t.Fatal("an id with a space was accepted")
	}
	if _, err := normalizeMachineID(nil); err == nil {
		t.Fatal("an empty id was accepted")
	}
}

func TestParseIOPlatformUUID(t *testing.T) {
	t.Parallel()
	const sample = `
+-o IOPlatformExpertDevice  <class IOPlatformExpertDevice, id 0x1000002f0>
  {
    "IOPlatformUUID" = "A1B2C3D4-E5F6-7890-ABCD-EF1234567890"
    "IOPlatformSerialNumber" = "C02XXXX"
  }
`
	id, err := parseIOPlatformUUID([]byte(sample))
	if err != nil || id != "A1B2C3D4-E5F6-7890-ABCD-EF1234567890" {
		t.Fatalf("id = %q, %v", id, err)
	}
	if _, err := parseIOPlatformUUID([]byte("nothing here")); err == nil {
		t.Fatal("missing UUID was accepted")
	}
}

func TestPlatformBinding(t *testing.T) {
	t.Parallel()
	switch runtime.GOOS {
	case "linux", "darwin", "windows":
	default:
		t.Skip("this OS has no machine binding")
	}
	b, err := platformBinding()
	if err != nil {
		t.Fatalf("platformBinding: %v", err)
	}
	prefix := runtime.GOOS + ":"
	if !bytes.HasPrefix(b, []byte(prefix)) || len(b) <= len(prefix)+8 {
		t.Fatalf("binding %q", b)
	}
	if strings.ContainsAny(string(b[len(prefix):]), " \t\r\n") {
		t.Fatalf("binding contains space: %q", b)
	}
}
