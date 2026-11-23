//go:build linux

package secrets

import (
	"fmt"
	"os"
)

// platformBinding returns this install's machine id. It is not a secret from
// other local users; file mode 0600 is what stops them reading the store.
func platformBinding() ([]byte, error) {
	var last error
	for _, path := range []string{"/etc/machine-id", "/var/lib/dbus/machine-id"} {
		b, err := os.ReadFile(path)
		if err != nil {
			last = err
			continue
		}
		id, err := normalizeMachineID(b)
		if err != nil {
			last = err
			continue
		}
		return []byte("linux:" + id), nil
	}
	if last == nil {
		last = os.ErrNotExist
	}
	return nil, fmt.Errorf("reading machine-id: %w", last)
}
