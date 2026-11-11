package hostlib

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/wei-dong/tako/internal/model"
	"github.com/wei-dong/tako/internal/secrets"
	"github.com/wei-dong/tako/internal/store"
)

// Version is the schema version of hosts.json.
const Version = 1

// Untagged is the name of the group holding hosts without tags.
const Untagged = ""

// Errors returned by a Library.
var (
	// ErrInUse means the host is a jump host of another host.
	ErrInUse = errors.New("host is used as a jump host")
	// ErrDuplicateName means another host already has the name.
	ErrDuplicateName = errors.New("a host with this name already exists")
)

// tagPalette is the design's tag color cycle (DESIGN.md §2.1).
var tagPalette = []string{"#E39A12", "#0FA3B1", "#3B7BF6", "#9C9CA6", "#8A63D2", "#E5484D"}

// file is the hosts.json document.
type file struct {
	Version int          `json:"version"`
	Hosts   []model.Host `json:"hosts"`
	Tags    []model.Tag  `json:"tags"`
}

// Group is the hosts under one tag, in the order the sidebar shows them.
type Group struct {
	// Tag is the group's tag; its Name is Untagged for hosts without tags.
	Tag   model.Tag
	Hosts []model.Host
}

// Library holds the hosts.
type Library struct {
	mu      sync.RWMutex
	hosts   []model.Host
	tags    []model.Tag
	doc     store.Doc
	saver   *store.Saver
	secrets secrets.Store
	onChg   []func()
}

// Open loads the library from doc. A missing file is an empty library. The
// secrets store is used to delete a host's secrets with it; it may be nil.
// onSaveErr receives background save errors.
func Open(doc store.Doc, sec secrets.Store, onSaveErr func(error)) (*Library, error) {
	doc.Version = Version
	l := &Library{doc: doc, secrets: sec}
	var f file
	err := doc.Load(&f)
	switch {
	case errors.Is(err, store.ErrNotExist):
	case err != nil:
		return nil, fmt.Errorf("loading hosts: %w", err)
	default:
		l.hosts, l.tags = f.Hosts, f.Tags
	}
	l.saver = store.NewSaver(300*time.Millisecond, l.save, onSaveErr)
	return l, nil
}

// OnChange registers fn to run (on an arbitrary goroutine) after any change.
func (l *Library) OnChange(fn func()) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.onChg = append(l.onChg, fn)
}

// Close writes pending changes and stops saving.
func (l *Library) Close() { l.saver.Close() }

// Flush writes pending changes now.
func (l *Library) Flush() { l.saver.Flush() }

func (l *Library) save() error {
	l.mu.RLock()
	f := file{Version: Version, Hosts: slices.Clone(l.hosts), Tags: slices.Clone(l.tags)}
	l.mu.RUnlock()
	if f.Hosts == nil {
		f.Hosts = []model.Host{}
	}
	if f.Tags == nil {
		f.Tags = []model.Tag{}
	}
	return l.doc.Save(f)
}

// changed schedules a save and tells watchers. The caller must have released
// l.mu.
func (l *Library) changed() {
	l.saver.Request()
	l.mu.RLock()
	fns := slices.Clone(l.onChg)
	l.mu.RUnlock()
	for _, fn := range fns {
		fn()
	}
}

// Hosts returns copies of all hosts in library order.
func (l *Library) Hosts() []model.Host {
	l.mu.RLock()
	defer l.mu.RUnlock()
	out := make([]model.Host, len(l.hosts))
	for i, h := range l.hosts {
		out[i] = h.Clone()
	}
	return out
}

// Len returns the number of hosts.
func (l *Library) Len() int {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return len(l.hosts)
}

// Get returns a copy of the host with the ID.
func (l *Library) Get(id model.ID) (model.Host, bool) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.getLocked(id)
}

func (l *Library) getLocked(id model.ID) (model.Host, bool) {
	i := slices.IndexFunc(l.hosts, func(h model.Host) bool { return h.ID == id })
	if i < 0 {
		return model.Host{}, false
	}
	return l.hosts[i].Clone(), true
}

// Add validates h, gives it an ID when it has none, and appends it. It returns
// the stored host.
func (l *Library) Add(h model.Host) (model.Host, error) {
	h = normalize(h)
	if err := h.Validate(); err != nil {
		return model.Host{}, fmt.Errorf("adding host: %w", err)
	}
	l.mu.Lock()
	if h.ID == "" {
		h.ID = model.NewID()
	}
	if err := l.checkNameLocked(h); err != nil {
		l.mu.Unlock()
		return model.Host{}, err
	}
	if _, exists := l.getLocked(h.ID); exists {
		l.mu.Unlock()
		return model.Host{}, fmt.Errorf("adding host: id %s already exists", h.ID)
	}
	l.hosts = append(l.hosts, h)
	l.ensureTagsLocked(h.Tags)
	l.mu.Unlock()
	l.changed()
	return h.Clone(), nil
}

// Update replaces the host with h.ID.
func (l *Library) Update(h model.Host) error {
	h = normalize(h)
	if err := h.Validate(); err != nil {
		return fmt.Errorf("updating host: %w", err)
	}
	l.mu.Lock()
	i := slices.IndexFunc(l.hosts, func(x model.Host) bool { return x.ID == h.ID })
	if i < 0 {
		l.mu.Unlock()
		return fmt.Errorf("updating host %s: %w", h.ID, model.ErrNotFound)
	}
	if err := l.checkNameLocked(h); err != nil {
		l.mu.Unlock()
		return err
	}
	if _, err := model.ResolveJumps(h, func(id model.ID) (model.Host, bool) { return l.getLocked(id) }); err != nil {
		l.mu.Unlock()
		return fmt.Errorf("updating host: %w", err)
	}
	old := l.hosts[i]
	l.hosts[i] = h
	l.ensureTagsLocked(h.Tags)
	l.mu.Unlock()
	l.dropOrphanedSecrets(old, h)
	l.changed()
	return nil
}

// Delete removes the host and its secrets. It fails with ErrInUse while
// another host jumps through it.
func (l *Library) Delete(ctx context.Context, id model.ID) error {
	l.mu.Lock()
	i := slices.IndexFunc(l.hosts, func(h model.Host) bool { return h.ID == id })
	if i < 0 {
		l.mu.Unlock()
		return fmt.Errorf("deleting host %s: %w", id, model.ErrNotFound)
	}
	for _, other := range l.hosts {
		if slices.ContainsFunc(other.Jump, func(hop model.Hop) bool { return hop.HostID == id }) {
			l.mu.Unlock()
			return fmt.Errorf("deleting %s: %w by %s", l.hosts[i].Name, ErrInUse, other.Name)
		}
	}
	gone := l.hosts[i]
	l.hosts = slices.Delete(l.hosts, i, i+1)
	l.mu.Unlock()
	var errs []error
	if l.secrets != nil {
		for _, ref := range gone.SecretRefs() {
			if err := l.secrets.Delete(ctx, ref); err != nil {
				errs = append(errs, err)
			}
		}
	}
	l.changed()
	if err := errors.Join(errs...); err != nil {
		return fmt.Errorf("deleting secrets of %s: %w", gone.Name, err)
	}
	return nil
}

// AddMany adds imported hosts, skipping those that already exist (same name,
// address, port and user). Jump references between the imported hosts are
// remapped onto the stored IDs. It returns the hosts added and the number
// skipped.
func (l *Library) AddMany(in []model.Host) ([]model.Host, int, error) {
	remap := make(map[model.ID]model.ID, len(in))
	var added []model.Host
	skipped := 0
	for _, h := range in {
		h = normalize(h)
		if existing, ok := l.findSame(h); ok {
			remap[h.ID] = existing.ID
			skipped++
			continue
		}
		oldID := h.ID
		h.ID = ""
		for j, hop := range h.Jump {
			if to, ok := remap[hop.HostID]; ok {
				h.Jump[j].HostID = to
			}
		}
		h.Name = l.uniqueName(h.Name)
		stored, err := l.Add(h)
		if err != nil {
			return added, skipped, fmt.Errorf("importing %s: %w", h.Name, err)
		}
		remap[oldID] = stored.ID
		added = append(added, stored)
	}
	return added, skipped, nil
}

func (l *Library) findSame(h model.Host) (model.Host, bool) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	for _, x := range l.hosts {
		if x.Name == h.Name && x.Addr == h.Addr && x.EffectivePort() == h.EffectivePort() && x.User == h.User {
			return x.Clone(), true
		}
	}
	return model.Host{}, false
}

func (l *Library) uniqueName(name string) string {
	l.mu.RLock()
	defer l.mu.RUnlock()
	taken := func(n string) bool {
		return slices.ContainsFunc(l.hosts, func(h model.Host) bool { return strings.EqualFold(h.Name, n) })
	}
	if !taken(name) {
		return name
	}
	for i := 2; ; i++ {
		if candidate := fmt.Sprintf("%s (%d)", name, i); !taken(candidate) {
			return candidate
		}
	}
}

// Tags returns the known tags in the order they were created.
func (l *Library) Tags() []model.Tag {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return slices.Clone(l.tags)
}

// TagColor returns the color of a tag, or "" for an unknown one.
func (l *Library) TagColor(name string) string {
	l.mu.RLock()
	defer l.mu.RUnlock()
	for _, t := range l.tags {
		if t.Name == name {
			return t.Color
		}
	}
	return ""
}

// SetTagColor changes a tag's color, creating the tag when needed.
func (l *Library) SetTagColor(name, color string) {
	l.mu.Lock()
	i := slices.IndexFunc(l.tags, func(t model.Tag) bool { return t.Name == name })
	if i < 0 {
		l.tags = append(l.tags, model.Tag{Name: name, Color: color})
	} else {
		l.tags[i].Color = color
	}
	l.mu.Unlock()
	l.changed()
}

// Groups returns the hosts grouped by their first tag. Groups follow tag
// creation order; hosts without tags come last under [Untagged]. The optional
// filter is applied as [Search] does.
func (l *Library) Groups(filter string) []Group {
	hosts := l.Search(filter)
	l.mu.RLock()
	defer l.mu.RUnlock()
	index := map[string]int{}
	var groups []Group
	for _, tag := range l.tags {
		index[tag.Name] = len(groups)
		groups = append(groups, Group{Tag: tag})
	}
	for _, h := range hosts {
		key := Untagged
		if len(h.Tags) > 0 {
			key = h.Tags[0]
		}
		gi, ok := index[key]
		if !ok {
			gi = len(groups)
			index[key] = gi
			groups = append(groups, Group{Tag: model.Tag{Name: key}})
		}
		groups[gi].Hosts = append(groups[gi].Hosts, h)
	}
	groups = slices.DeleteFunc(groups, func(g Group) bool { return len(g.Hosts) == 0 })
	slices.SortStableFunc(groups, func(a, b Group) int {
		return cmp.Compare(boolInt(a.Tag.Name == Untagged), boolInt(b.Tag.Name == Untagged))
	})
	return groups
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// Search returns the hosts matching query. Every whitespace-separated term
// must match the name, address, user or a tag, case-insensitively; a term
// starting with "#" matches tags only. An empty query returns every host.
func (l *Library) Search(query string) []model.Host {
	terms := strings.Fields(strings.ToLower(query))
	all := l.Hosts()
	if len(terms) == 0 {
		return all
	}
	return slices.DeleteFunc(all, func(h model.Host) bool {
		return !matchesAll(h, terms)
	})
}

func matchesAll(h model.Host, terms []string) bool {
	for _, term := range terms {
		if !matches(h, term) {
			return false
		}
	}
	return true
}

func matches(h model.Host, term string) bool {
	if tag, ok := strings.CutPrefix(term, "#"); ok {
		return slices.ContainsFunc(h.Tags, func(t string) bool { return strings.Contains(strings.ToLower(t), tag) })
	}
	for _, field := range []string{h.Name, h.Addr, h.User} {
		if strings.Contains(strings.ToLower(field), term) {
			return true
		}
	}
	return slices.ContainsFunc(h.Tags, func(t string) bool { return strings.Contains(strings.ToLower(t), term) })
}

func (l *Library) checkNameLocked(h model.Host) error {
	for _, x := range l.hosts {
		if x.ID != h.ID && strings.EqualFold(x.Name, h.Name) {
			return fmt.Errorf("%w: %s", ErrDuplicateName, h.Name)
		}
	}
	return nil
}

func (l *Library) ensureTagsLocked(names []string) {
	for _, name := range names {
		if slices.ContainsFunc(l.tags, func(t model.Tag) bool { return t.Name == name }) {
			continue
		}
		color := tagPalette[len(l.tags)%len(tagPalette)]
		l.tags = append(l.tags, model.Tag{Name: name, Color: color})
	}
}

// dropOrphanedSecrets deletes secrets an edit stopped referencing.
func (l *Library) dropOrphanedSecrets(old, updated model.Host) {
	if l.secrets == nil {
		return
	}
	keep := updated.SecretRefs()
	for _, ref := range old.SecretRefs() {
		if !slices.Contains(keep, ref) {
			_ = l.secrets.Delete(context.Background(), ref) //nolint:errcheck // best effort: a stale keychain entry is harmless
		}
	}
}

// normalize trims user input and fills defaults.
func normalize(h model.Host) model.Host {
	h = h.Clone()
	h.Name = strings.TrimSpace(h.Name)
	h.Addr = strings.TrimSpace(h.Addr)
	h.User = strings.TrimSpace(h.User)
	if h.OS == "" {
		h.OS = model.OSLinux
	}
	tags := h.Tags[:0]
	for _, t := range h.Tags {
		if t = strings.TrimSpace(t); t != "" && !slices.Contains(tags, t) {
			tags = append(tags, t)
		}
	}
	h.Tags = tags
	return h
}

// Export returns the library as JSON for sharing: the same document as
// hosts.json but with every keychain reference removed, so it never contains
// or points at a secret.
func (l *Library) Export() ([]byte, error) {
	l.mu.RLock()
	f := file{Version: Version, Hosts: make([]model.Host, len(l.hosts)), Tags: slices.Clone(l.tags)}
	for i, h := range l.hosts {
		h = h.Clone()
		for j := range h.Auth {
			h.Auth[j].SecretRef = ""
		}
		if h.Proxy != nil {
			h.Proxy.SecretRef = ""
		}
		f.Hosts[i] = h
	}
	l.mu.RUnlock()
	return marshalIndented(f)
}
