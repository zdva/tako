//go:build darwin

package secrets

import (
	"context"
	"fmt"
	"os/exec"
	"time"
)

// platformBinding returns the hardware platform UUID. ioreg does not talk to
// the keychain and does not prompt.
func platformBinding() ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "/usr/sbin/ioreg", "-rd1", "-c", "IOPlatformExpertDevice").Output()
	if err != nil {
		return nil, fmt.Errorf("reading the platform UUID: %w", err)
	}
	id, err := parseIOPlatformUUID(out)
	if err != nil {
		return nil, fmt.Errorf("reading the platform UUID: %w", err)
	}
	return []byte("darwin:" + id), nil
}
