package secrets

import (
	"context"
	"errors"
	"fmt"
	"slices"
)

// MigrateResult counts what [Migrate] did.
type MigrateResult struct {
	// Copied is how many values were written to the destination.
	Copied int
	// Missing is how many references the source did not have.
	Missing int
	// Deleted is how many copied references were removed from the source.
	Deleted int
	// DeleteErrs is how many of those deletes failed.
	DeleteErrs int
}

// Migrate copies refs from src into dst, then deletes the copied refs from
// src. A reference src does not have is skipped. Every read happens before
// any write, and src is only changed after every write has succeeded, so a
// failure leaves src as it was. Values are not included in errors.
func Migrate(ctx context.Context, dst, src Store, refs []string) (MigrateResult, error) {
	var result MigrateResult
	type item struct {
		ref string
		val []byte
	}
	held := make([]item, 0, len(refs))
	defer func() {
		for i := range held {
			clear(held[i].val)
		}
	}()
	for _, ref := range uniqueRefs(refs) {
		if err := ctx.Err(); err != nil {
			return result, fmt.Errorf("migrating secrets: %w", err)
		}
		val, err := src.Get(ctx, ref)
		if errors.Is(err, ErrNotFound) {
			result.Missing++
			continue
		}
		if err != nil {
			return result, fmt.Errorf("reading %s from the old store: %w", ref, err)
		}
		held = append(held, item{ref: ref, val: val})
	}
	for _, it := range held {
		if err := dst.Set(ctx, it.ref, it.val); err != nil {
			return result, fmt.Errorf("writing %s into the encrypted store: %w", it.ref, err)
		}
		result.Copied++
	}
	var delErr error
	for _, it := range held {
		if err := src.Delete(ctx, it.ref); err != nil {
			result.DeleteErrs++
			delErr = errors.Join(delErr, err)
			continue
		}
		result.Deleted++
	}
	if delErr != nil {
		return result, fmt.Errorf("copied %d secrets, but removing them from the old store failed: %w", result.Copied, delErr)
	}
	return result, nil
}

func uniqueRefs(refs []string) []string {
	seen := make(map[string]struct{}, len(refs))
	out := make([]string, 0, len(refs))
	for _, ref := range refs {
		if ref == "" {
			continue
		}
		if _, ok := seen[ref]; ok {
			continue
		}
		seen[ref] = struct{}{}
		out = append(out, ref)
	}
	slices.Sort(out)
	return out
}
