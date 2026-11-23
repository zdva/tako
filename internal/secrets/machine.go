package secrets

import (
	"errors"
	"strings"
)

// normalizeMachineID trims a machine-id file. The id is not a password: on
// Linux it is world-readable. It only stops a copied secret file from
// decrypting on another machine.
func normalizeMachineID(b []byte) (string, error) {
	id := strings.TrimSpace(string(b))
	if id == "" || strings.ContainsAny(id, " \t\r\n") {
		return "", errors.New("empty machine id")
	}
	return id, nil
}

// parseIOPlatformUUID reads the quoted IOPlatformUUID from ioreg text.
func parseIOPlatformUUID(out []byte) (string, error) {
	_, rest, ok := strings.Cut(string(out), `"IOPlatformUUID" = "`)
	if !ok {
		return "", errors.New("platform UUID not found")
	}
	id, _, ok := strings.Cut(rest, `"`)
	if !ok || id == "" || strings.ContainsAny(id, " \t\r\n") {
		return "", errors.New("platform UUID not found")
	}
	return id, nil
}
