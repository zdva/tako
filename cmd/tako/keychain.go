package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/egoist/mygo"

	"github.com/zdva/tako/internal/secrets"
)

// systemCredentialStore is the operating system's credential store.
// Only --migrate-keychain constructs it. Launching the app must not:
// a read can ask the user to unlock the macOS keychain, which is the
// prompt this store exists to retire.
type systemCredentialStore struct {
	module mygo.SecretsModule
}

func newSystemCredentialStore() *systemCredentialStore {
	return &systemCredentialStore{module: mygo.Secrets.Service(secrets.LegacyService)}
}

func (s *systemCredentialStore) Get(ctx context.Context, ref string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("reading secret: %w", err)
	}
	v, err := s.module.Get(ref)
	if errors.Is(err, mygo.ErrSecretNotFound) {
		return nil, secrets.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("reading secret from the system credential store: %w", err)
	}
	return v, nil
}

func (s *systemCredentialStore) Set(ctx context.Context, ref string, value []byte) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("storing secret: %w", err)
	}
	if err := s.module.Set(ref, value); err != nil {
		return fmt.Errorf("storing secret in the system credential store: %w", err)
	}
	return nil
}

func (s *systemCredentialStore) Delete(ctx context.Context, ref string) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("deleting secret: %w", err)
	}
	if err := s.module.Delete(ref); err != nil {
		return fmt.Errorf("deleting secret from the system credential store: %w", err)
	}
	return nil
}
