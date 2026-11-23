package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"

	"github.com/zdva/tako/internal/hostlib"
	"github.com/zdva/tako/internal/secrets"
	"github.com/zdva/tako/internal/store"
)

// migrateFromSystem copies secrets referenced by hosts.json out of the
// system credential store. It is not part of launching the app.
func migrateFromSystem(ctx context.Context) error {
	dirs, err := store.DefaultDirs()
	if err != nil {
		return fmt.Errorf("finding data directories: %w", err)
	}
	return runMigrate(ctx, dirs, newSystemCredentialStore(), os.Stderr)
}

// runMigrate copies every secret reference in hosts.json from src into the
// encrypted file, then deletes the copied values from src. src is not called
// when hosts.json has no references.
func runMigrate(ctx context.Context, dirs store.Dirs, src secrets.Store, out io.Writer) error {
	if err := dirs.Ensure(); err != nil {
		return fmt.Errorf("preparing data directories: %w", err)
	}
	lock, err := store.Acquire(dirs.Config)
	if err != nil {
		return fmt.Errorf("another Tako instance is using %s: %w", dirs.Config, err)
	}
	defer func() { _ = lock.Release() }()

	refs, err := secretRefs(dirs)
	if err != nil {
		return err
	}
	say := func(format string, args ...any) error {
		if _, werr := fmt.Fprintf(out, format, args...); werr != nil {
			return fmt.Errorf("writing the migration report: %w", werr)
		}
		return nil
	}
	if len(refs) == 0 {
		if err := say("No saved passwords are referenced from hosts.json, so nothing was copied.\n"); err != nil {
			return err
		}
		if err := say("Tako no longer reads the system credential store. Type a password again in the app and choose \"Remember in Tako\".\n"); err != nil {
			return err
		}
		return say("Leftover items for the service %s can be deleted in Keychain Access, Credential Manager, or the Secret Service.\n", secrets.LegacyService)
	}
	if err := say("Reading passwords from the system credential store (service %s) into Tako's encrypted file.\n", secrets.LegacyService); err != nil {
		return err
	}
	if err := say("The system may ask you to allow this, once per saved password. After a successful copy, those items are removed.\n"); err != nil {
		return err
	}

	dst, err := secrets.Open(dirs.Secrets())
	switch {
	case err == nil:
	case errors.Is(err, secrets.ErrSealed):
		if werr := say("The existing encrypted store could not be read and was set aside (%v).\n", err); werr != nil {
			return werr
		}
	default:
		return fmt.Errorf("opening the encrypted store: %w", err)
	}
	result, err := secrets.Migrate(ctx, dst, src, refs)
	if cerr := dst.Close(); cerr != nil {
		err = errors.Join(err, cerr)
	}
	if err != nil {
		return err
	}
	if err := say("Copied %d secret(s) into the encrypted store and removed them from the system credential store.\n", result.Copied); err != nil {
		return err
	}
	if result.Missing > 0 {
		return say("%d reference(s) had nothing stored.\n", result.Missing)
	}
	return nil
}

func secretRefs(dirs store.Dirs) ([]string, error) {
	lib, err := hostlib.Open(store.Doc{Path: dirs.Hosts(), Version: hostlib.Version}, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("opening the host library: %w", err)
	}
	defer lib.Close()
	seen := map[string]struct{}{}
	var refs []string
	for _, h := range lib.Hosts() {
		for _, ref := range h.SecretRefs() {
			if ref == "" {
				continue
			}
			if _, ok := seen[ref]; ok {
				continue
			}
			seen[ref] = struct{}{}
			refs = append(refs, ref)
		}
	}
	slices.Sort(refs)
	return refs, nil
}
