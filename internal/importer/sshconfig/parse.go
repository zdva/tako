package sshconfig

import (
	"bufio"
	"cmp"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/wei-dong/tako/internal/model"
)

// MaxIncludeDepth limits nested Include directives, which guards against
// include loops.
const MaxIncludeDepth = 8

// maxLine bounds one line so hostile input cannot exhaust memory.
const maxLine = 64 * 1024

// Warning is something in the file that could not be imported faithfully.
type Warning struct {
	// File and Line locate the directive; Line is 1-based.
	File string
	Line int
	Msg  string
}

func (w Warning) String() string { return fmt.Sprintf("%s:%d: %s", w.File, w.Line, w.Msg) }

// Source reads configuration files. The zero value is not usable; use
// [OSSource] or supply your own for tests.
type Source interface {
	ReadFile(name string) ([]byte, error)
	Glob(pattern string) ([]string, error)
}

// OSSource reads from the real file system.
type OSSource struct{}

// ReadFile implements [Source].
func (OSSource) ReadFile(name string) ([]byte, error) {
	data, err := os.ReadFile(name)
	if err != nil {
		return nil, fmt.Errorf("reading ssh config: %w", err)
	}
	return data, nil
}

// Glob implements [Source].
func (OSSource) Glob(pattern string) ([]string, error) {
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("expanding %q: %w", pattern, err)
	}
	return matches, nil
}

// Options configure a parse.
type Options struct {
	// Source reads files; nil means the real file system.
	Source Source
	// Home expands "~" in Include and IdentityFile paths ("~/" is kept as is
	// in key paths so the stored host stays portable).
	Home string
	// BaseDir resolves relative Include paths; OpenSSH uses ~/.ssh.
	BaseDir string
}

// block is one Host section with its options.
type block struct {
	patterns []string
	opts     map[string][]string // lower-case keyword -> values in file order
	file     string
	line     int
}

// ParseFile imports hosts from the configuration at name.
func ParseFile(name string, o Options) ([]model.Host, []Warning, error) {
	if o.Source == nil {
		o.Source = OSSource{}
	}
	if o.BaseDir == "" {
		o.BaseDir = filepath.Dir(name)
	}
	data, err := o.Source.ReadFile(name)
	if err != nil {
		return nil, nil, err
	}
	return Parse(strings.NewReader(string(data)), name, o)
}

// Parse imports hosts from r; name is used in warnings and to resolve nothing
// else. Hosts get fresh IDs and jump hosts referenced by ProxyJump are added
// to the result.
func Parse(r io.Reader, name string, o Options) ([]model.Host, []Warning, error) {
	if o.Source == nil {
		o.Source = OSSource{}
	}
	p := &parser{o: o}
	if err := p.parseReader(r, name, 0); err != nil {
		return nil, p.warnings, err
	}
	hosts := p.build()
	return hosts, p.warnings, nil
}

type parser struct {
	o        Options
	blocks   []*block // blocks[0] holds options before any Host line, matching all hosts
	cur      *block
	skipping bool // inside a Match block, whose options are ignored
	warnings []Warning
}

func (p *parser) warn(file string, line int, format string, args ...any) {
	p.warnings = append(p.warnings, Warning{File: file, Line: line, Msg: fmt.Sprintf(format, args...)})
}

func (p *parser) startBlock(patterns []string, file string, line int) {
	b := &block{patterns: patterns, opts: map[string][]string{}, file: file, line: line}
	p.blocks = append(p.blocks, b)
	p.cur = b
}

func (p *parser) parseReader(r io.Reader, name string, depth int) error {
	if p.cur == nil {
		p.startBlock([]string{"*"}, name, 0)
	}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 4096), maxLine)
	lineNo := 0
	for sc.Scan() {
		lineNo++
		key, args := splitDirective(sc.Text())
		if key == "" {
			continue
		}
		if err := p.directive(name, lineNo, key, args, depth); err != nil {
			return err
		}
	}
	if err := sc.Err(); err != nil {
		return fmt.Errorf("reading %s: %w", name, err)
	}
	return nil
}

func (p *parser) directive(file string, line int, key string, args []string, depth int) error {
	switch key {
	case "host":
		p.skipping = false
		p.startBlock(args, file, line)
	case "match":
		p.skipping = true
		p.startBlock(nil, file, line)
		p.warn(file, line, "Match blocks are not imported")
	case "include":
		return p.include(file, line, args, depth)
	default:
		if p.skipping || len(args) == 0 {
			return nil
		}
		p.cur.opts[key] = append(p.cur.opts[key], args...)
	}
	return nil
}

func (p *parser) include(file string, line int, patterns []string, depth int) error {
	if depth >= MaxIncludeDepth {
		p.warn(file, line, "Include nesting deeper than %d is ignored", MaxIncludeDepth)
		return nil
	}
	for _, pat := range patterns {
		pat = p.expand(pat)
		if !filepath.IsAbs(pat) {
			pat = filepath.Join(p.o.BaseDir, pat)
		}
		matches, err := p.o.Source.Glob(pat)
		if err != nil {
			p.warn(file, line, "bad Include pattern %q", pat)
			continue
		}
		slices.Sort(matches)
		for _, m := range matches {
			data, err := p.o.Source.ReadFile(m)
			if err != nil {
				if !errors.Is(err, fs.ErrNotExist) {
					p.warn(file, line, "cannot read included file %s", m)
				}
				continue
			}
			// An Include inside a Host block stays in that block in OpenSSH;
			// files included at the top level start in the global block.
			saved, savedSkip := p.cur, p.skipping
			if err := p.parseReader(strings.NewReader(string(data)), m, depth+1); err != nil {
				return err
			}
			p.cur, p.skipping = saved, savedSkip
		}
	}
	return nil
}

func (p *parser) expand(s string) string {
	if p.o.Home == "" {
		return s
	}
	if s == "~" {
		return p.o.Home
	}
	if rest, ok := strings.CutPrefix(s, "~/"); ok {
		return filepath.Join(p.o.Home, rest)
	}
	return s
}

// splitDirective splits "Keyword value", "Keyword=value" or quoted values
// into a lower-cased keyword and its arguments. Comments and blanks give "".
func splitDirective(line string) (string, []string) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return "", nil
	}
	i := strings.IndexAny(line, " \t=")
	if i < 0 {
		return strings.ToLower(line), nil
	}
	key := strings.ToLower(line[:i])
	rest := strings.TrimLeft(line[i:], " \t")
	rest = strings.TrimPrefix(rest, "=")
	return key, splitArgs(strings.TrimLeft(rest, " \t"))
}

// splitArgs splits on whitespace, honoring double quotes, and stops at a
// comment that starts a token.
func splitArgs(s string) []string {
	var args []string
	var cur strings.Builder
	inQuote, has := false, false
	flush := func() {
		if has {
			args = append(args, cur.String())
		}
		cur.Reset()
		has = false
	}
	for _, r := range s {
		switch {
		case r == '"':
			inQuote = !inQuote
			has = true
		case !inQuote && (r == ' ' || r == '\t'):
			flush()
		case !inQuote && r == '#' && !has:
			flush()
			return args
		default:
			cur.WriteRune(r)
			has = true
		}
	}
	flush()
	return args
}

// build turns the parsed blocks into hosts.
func (p *parser) build() []model.Host {
	var aliases []string
	seen := map[string]bool{}
	for _, b := range p.blocks {
		for _, pat := range b.patterns {
			if isConcrete(pat) && !seen[pat] {
				seen[pat] = true
				aliases = append(aliases, pat)
			}
		}
	}
	byAlias := map[string]*model.Host{}
	var hosts []*model.Host
	var lookup func(alias string) *model.Host
	lookup = func(alias string) *model.Host {
		if h, ok := byAlias[alias]; ok {
			return h
		}
		return nil
	}
	for _, alias := range aliases {
		h := p.hostFor(alias)
		byAlias[alias] = h
		hosts = append(hosts, h)
	}
	// Resolve ProxyJump now that every alias has a host.
	for _, alias := range aliases {
		h := byAlias[alias]
		jumps := p.value(alias, "proxyjump")
		if jumps == "" || strings.EqualFold(jumps, "none") {
			continue
		}
		for _, spec := range strings.Split(jumps, ",") {
			spec = strings.TrimSpace(spec)
			if spec == "" {
				continue
			}
			if jh := lookup(spec); jh != nil && jh != h {
				h.Jump = append(h.Jump, model.Hop{HostID: jh.ID})
				continue
			}
			jh := p.literalJump(spec, h)
			hosts = append(hosts, jh)
			byAlias[spec] = jh
			h.Jump = append(h.Jump, model.Hop{HostID: jh.ID})
		}
	}
	// A chain is the whole path (model.ResolveJumps does not follow a hop's
	// own Jump), so flatten nested chains: "a via b" where b is "via c" becomes
	// c, b. Hosts caught in a cycle lose their jumps.
	out := make([]model.Host, 0, len(hosts))
	byID := map[model.ID]*model.Host{}
	for _, h := range hosts {
		byID[h.ID] = h
	}
	cyclic := map[model.ID]bool{}
	for _, h := range hosts {
		if reaches(h.ID, h.Jump, byID, map[model.ID]bool{}) {
			cyclic[h.ID] = true
			p.warn(p.blockFile(h.Name), 0, "host %s jumps through itself; its ProxyJump is ignored", h.Name)
		}
	}
	for _, h := range hosts {
		if cyclic[h.ID] {
			h.Jump = nil
		}
	}
	for _, h := range hosts {
		h.Jump = flattenJumps(h, byID)
	}
	for _, h := range hosts {
		out = append(out, *h)
	}
	return out
}

// reaches reports whether target can be reached by following hops.
func reaches(target model.ID, hops []model.Hop, byID map[model.ID]*model.Host, seen map[model.ID]bool) bool {
	for _, hop := range hops {
		if hop.HostID == target {
			return true
		}
		if seen[hop.HostID] {
			continue
		}
		seen[hop.HostID] = true
		if jh, ok := byID[hop.HostID]; ok && reaches(target, jh.Jump, byID, seen) {
			return true
		}
	}
	return false
}

func flattenJumps(h *model.Host, byID map[model.ID]*model.Host) []model.Hop {
	var chain []model.Hop
	seen := map[model.ID]bool{h.ID: true}
	var add func(hops []model.Hop)
	add = func(hops []model.Hop) {
		for _, hop := range hops {
			if seen[hop.HostID] {
				continue
			}
			seen[hop.HostID] = true
			if jh, ok := byID[hop.HostID]; ok {
				add(jh.Jump)
			}
			chain = append(chain, hop)
		}
	}
	add(h.Jump)
	return chain
}

// isConcrete reports whether a Host pattern names one host (no wildcards or
// negation).
func isConcrete(pat string) bool {
	return pat != "" && !strings.ContainsAny(pat, "*?!")
}

// matchesAlias reports whether one of the patterns applies to alias, honoring
// negated patterns.
func matchesAlias(patterns []string, alias string) bool {
	matched := false
	for _, pat := range patterns {
		neg := strings.HasPrefix(pat, "!")
		ok, err := path.Match(strings.TrimPrefix(pat, "!"), alias)
		if err != nil || !ok {
			continue
		}
		if neg {
			return false
		}
		matched = true
	}
	return matched
}

// value returns the first value of an option over all blocks that match the
// alias, in file order, as OpenSSH does.
func (p *parser) value(alias, key string) string {
	for _, b := range p.blocks {
		if !matchesAlias(b.patterns, alias) {
			continue
		}
		if vals := b.opts[key]; len(vals) > 0 {
			return vals[0]
		}
	}
	return ""
}

// values returns every value of a repeatable option from matching blocks.
func (p *parser) values(alias, key string) []string {
	var out []string
	for _, b := range p.blocks {
		if matchesAlias(b.patterns, alias) {
			out = append(out, b.opts[key]...)
		}
	}
	return out
}

func (p *parser) hostFor(alias string) *model.Host {
	h := &model.Host{
		ID:   model.NewID(),
		Name: alias,
		Addr: cmp.Or(p.value(alias, "hostname"), alias),
		User: cmp.Or(p.value(alias, "user"), currentUser()),
		OS:   model.OSLinux,
	}
	h.Addr = strings.ReplaceAll(h.Addr, "%h", alias)
	if port, err := strconv.Atoi(p.value(alias, "port")); err == nil && port > 0 && port != model.DefaultPort {
		h.Port = port
	}
	if ka, err := strconv.Atoi(p.value(alias, "serveraliveinterval")); err == nil && ka > 0 {
		h.KeepAlive = ka
	}
	for _, id := range p.values(alias, "identityfile") {
		if strings.Contains(id, "%") || strings.EqualFold(id, "none") {
			continue
		}
		h.Auth = append(h.Auth, model.Auth{Kind: model.AuthKey, KeyPath: id})
	}
	h.Auth = append(h.Auth, model.Auth{Kind: model.AuthAgent}, model.Auth{Kind: model.AuthPassword})
	if p.value(alias, "proxycommand") != "" && p.value(alias, "proxyjump") == "" {
		p.warn(p.blockFile(alias), 0, "host %s uses ProxyCommand, which is not imported", alias)
	}
	return h
}

func (p *parser) blockFile(alias string) string {
	for _, b := range p.blocks {
		if slices.Contains(b.patterns, alias) {
			return b.file
		}
	}
	return ""
}

// literalJump makes a host for a ProxyJump entry that is not a Host alias:
// "[user@]host[:port]".
func (p *parser) literalJump(spec string, from *model.Host) *model.Host {
	user := from.User
	if u, rest, ok := strings.Cut(spec, "@"); ok {
		user, spec = u, rest
	}
	addr, port := spec, 0
	if hh, pp, ok := strings.Cut(spec, ":"); ok && !strings.Contains(pp, ":") {
		if n, err := strconv.Atoi(pp); err == nil {
			addr, port = hh, n
		}
	}
	h := &model.Host{
		ID: model.NewID(), Name: addr, Addr: addr, User: user, OS: model.OSLinux,
		Auth: []model.Auth{{Kind: model.AuthAgent}, {Kind: model.AuthPassword}},
	}
	if port != model.DefaultPort {
		h.Port = port
	}
	return h
}

func currentUser() string {
	for _, k := range []string{"USER", "USERNAME", "LOGNAME"} {
		if v := os.Getenv(k); v != "" {
			return v
		}
	}
	return "root"
}
