package transfer

import (
	"context"
	"fmt"
	"io/fs"
	"slices"
	"strings"
)

// item is one thing to create at the destination: a directory or a file.
// rel is the path below the job's root, written with "/"; the root itself
// (a single file, or the top directory) is "".
type item struct {
	rel  string
	size int64
	dir  bool
}

// plan lists what a source holds. Links are not followed and not copied
// (following them risks loops and copying more than was asked); their count
// is returned.
func plan(ctx context.Context, ep Endpoint, root string) (items []item, links int, err error) {
	efs := ep.FS()
	fi, err := efs.Stat(root)
	if err != nil {
		return nil, 0, err
	}
	if !fi.IsDir() {
		return []item{{size: fi.Size()}}, 0, nil
	}
	items = append(items, item{dir: true})
	var walk func(dir, rel string) error
	walk = func(dir, rel string) error {
		if err := context.Cause(ctx); err != nil {
			return err
		}
		list, err := efs.ReadDir(dir)
		if err != nil {
			return err
		}
		slices.SortFunc(list, func(a, b fs.FileInfo) int { return strings.Compare(a.Name(), b.Name()) })
		for _, e := range list {
			childRel := e.Name()
			if rel != "" {
				childRel = rel + "/" + e.Name()
			}
			switch {
			case e.Mode()&fs.ModeSymlink != 0:
				links++
			case e.IsDir():
				items = append(items, item{rel: childRel, dir: true})
				if err := walk(efs.Join(dir, e.Name()), childRel); err != nil {
					return err
				}
			case e.Mode().IsRegular():
				items = append(items, item{rel: childRel, size: e.Size()})
			default:
				links++ // devices, sockets and pipes are not files to copy
			}
		}
		return nil
	}
	if err := walk(root, ""); err != nil {
		return nil, 0, fmt.Errorf("listing %s: %w", root, err)
	}
	return items, links, nil
}

// at joins rel (written with "/") onto root in the file system's own style.
func at(e Endpoint, root, rel string) string {
	if rel == "" {
		return root
	}
	return e.FS().Join(append([]string{root}, strings.Split(rel, "/")...)...)
}

func totalSize(items []item) (n int64) {
	for _, it := range items {
		n += it.size
	}
	return n
}
