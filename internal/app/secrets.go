package app

import (
	"errors"
	"fmt"
	"log/slog"

	"github.com/zdva/tako/internal/secrets"
	"github.com/zdva/tako/internal/store"
)

// openSecretStore returns the store the process will use. The system
// credential store is never opened here: reading it can ask the user to
// unlock the macOS keychain.
func openSecretStore(dirs store.Dirs, override secrets.Store) (secrets.Store, error) {
	if override != nil {
		return override, nil
	}
	st, err := secrets.Open(dirs.Secrets())
	switch {
	case err == nil:
		return st, nil
	case errors.Is(err, secrets.ErrSealed):
		slog.Warn("saved passwords could not be decrypted and were set aside; enter them again", "err", err)
		return st, nil
	case errors.Is(err, secrets.ErrBindingUnavailable):
		slog.Warn("machine id unavailable; passwords kept for this session only", "err", err)
		return secrets.NewEphemeral(), nil
	default:
		return nil, fmt.Errorf("opening the secret store: %w", err)
	}
}
