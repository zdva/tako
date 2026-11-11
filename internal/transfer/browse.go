package transfer

import (
	"context"
	"fmt"
	"io/fs"
	"slices"
	"strings"
	"time"
)

// Entry is one line of a directory listing.
type Entry struct {
	Name    string
	Size    int64
	ModTime time.Time
	Dir     bool
	Link    bool
}

// List reads a directory: folders first, then files, each by name.
func List(ctx context.Context, ep Endpoint, dir string) ([]Entry, error) {
	if err := context.Cause(ctx); err != nil {
		return nil, fmt.Errorf("listing %s: %w", dir, err)
	}
	infos, err := ep.FS().ReadDir(dir)
	if err != nil {
		return nil, err
	}
	out := make([]Entry, 0, len(infos))
	for _, fi := range infos {
		e := Entry{Name: fi.Name(), Size: fi.Size(), ModTime: fi.ModTime(), Dir: fi.IsDir(), Link: fi.Mode()&fs.ModeSymlink != 0}
		if e.Link { // show a link to a folder as a folder
			if st, err := ep.FS().Stat(ep.FS().Join(dir, fi.Name())); err == nil {
				e.Dir, e.Size, e.ModTime = st.IsDir(), st.Size(), st.ModTime()
			}
		}
		out = append(out, e)
	}
	slices.SortFunc(out, func(a, b Entry) int {
		if a.Dir != b.Dir {
			if a.Dir {
				return -1
			}
			return 1
		}
		return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	})
	return out, nil
}

// Parent is the directory above dir, in the style of ep's file system.
func Parent(ep Endpoint, dir string) string {
	p := ep.FS().Join(dir, "..")
	if p == "" || p == "." {
		return dir
	}
	return p
}
