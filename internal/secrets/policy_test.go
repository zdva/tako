package secrets

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The running app must not call MyGo's credential store. The only allowed
// call site is the explicit one-time migration command.
func TestProductionCodeDoesNotCallTheCredentialStore(t *testing.T) {
	t.Parallel()
	root := filepath.Clean(filepath.Join("..", ".."))
	needle := "mygo" + ".Secrets"
	var hits []string
	for _, dir := range []string{filepath.Join(root, "internal"), filepath.Join(root, "cmd")} {
		err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") {
				return err
			}
			if filepath.ToSlash(path) == "cmd/tako/keychain.go" || strings.HasSuffix(filepath.ToSlash(path), "/cmd/tako/keychain.go") {
				return nil
			}
			data, rerr := os.ReadFile(path)
			if rerr != nil {
				return rerr
			}
			if strings.Contains(string(data), needle) {
				hits = append(hits, path)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(hits) > 0 {
		t.Fatalf("these files call the system credential store: %s", strings.Join(hits, ", "))
	}
}
