package hostfile

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf16"

	"github.com/zdva/tako/internal/model"
)

// Limits that keep hostile or mistaken input from exhausting memory.
const (
	// MaxFileSize bounds one file; a host list of 100,000 rows is far smaller.
	MaxFileSize = 32 << 20
	// MaxFiles bounds the number of session files read from an archive or folder.
	MaxFiles = 20000
	// MaxTotal bounds all the files of an archive or folder together, so a
	// zip bomb cannot exhaust memory.
	MaxTotal = 128 << 20
	// MaxHosts bounds the hosts one import returns.
	MaxHosts = 100000
)

// Format is a file format Tako can import.
type Format string

// The formats.
const (
	FormatTako      Format = "tako"
	FormatXshell    Format = "xshell"
	FormatSecureCRT Format = "securecrt"
	FormatTermius   Format = "termius"
)

// Errors.
var (
	ErrUnknownFormat = errors.New("not a Tako, Xshell, SecureCRT or Termius file")
	ErrTooLarge      = errors.New("file is too large")
	ErrEmpty         = errors.New("no hosts found")
)

// Options tune an import.
type Options struct {
	// DefaultUser is the login for sessions that have none. Without it such
	// sessions are skipped, since a host needs a user name.
	DefaultUser string
}

// Result is what a file contained.
type Result struct {
	Format Format
	Hosts  []model.Host
	// Tags are the tag colors of a Tako export; other formats have none.
	Tags []model.Tag
	// Warnings say what was left out or changed, one line each.
	Warnings []string
	// Passwords counts the hosts whose file held a saved password that was
	// not imported.
	Passwords int
}

func (r *Result) warn(format string, args ...any) {
	if len(r.Warnings) < 200 {
		r.Warnings = append(r.Warnings, fmt.Sprintf(format, args...))
	}
}

// Detect names the format of a file from its name and content.
func Detect(name string, data []byte) (Format, error) {
	switch {
	case bytes.HasPrefix(data, []byte("PK\x03\x04")):
		return FormatXshell, nil // an .xts archive is a zip of .xsh files
	case isUTF16(data):
		return FormatXshell, nil
	}
	text := string(bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF}))
	head := text[:min(len(text), 4096)]
	trimmed := strings.TrimSpace(head)
	switch {
	case strings.HasPrefix(trimmed, "{") && strings.Contains(text, `"hosts"`):
		return FormatTako, nil
	case strings.HasPrefix(trimmed, "<") && strings.Contains(text, "Hostname"):
		return FormatSecureCRT, nil
	case strings.Contains(text, `S:"Hostname"=`) || strings.Contains(text, `D:"[SSH2] Port"=`):
		return FormatSecureCRT, nil
	case strings.Contains(text, "[CONNECTION]") || (strings.Contains(text, "[General]") && strings.Contains(text, "Host=")):
		return FormatXshell, nil
	case looksLikeTermius(head):
		return FormatTermius, nil
	}
	return "", fmt.Errorf("%w: %s", ErrUnknownFormat, filepath.Base(name))
}

func looksLikeTermius(head string) bool {
	first, _, _ := strings.Cut(head, "\n")
	cols := splitHeader(first)
	return slices.ContainsFunc(cols, isHostCol) && (slices.Contains(cols, "label") || slices.Contains(cols, "groups") || slices.Contains(cols, "username"))
}

// Parse reads one file's content. name is its file name, used for session
// names and format detection.
func Parse(name string, data []byte, o Options) (Result, error) {
	if len(data) > MaxFileSize {
		return Result{}, fmt.Errorf("%w: %d bytes", ErrTooLarge, len(data))
	}
	f, err := Detect(name, data)
	if err != nil {
		return Result{}, err
	}
	var res Result
	switch f {
	case FormatTako:
		res, err = parseTako(data)
	case FormatXshell:
		res, err = parseXshellAny(name, data, o)
	case FormatSecureCRT:
		res, err = parseSecureCRTAny(name, data, o)
	case FormatTermius:
		res, err = parseTermius(data, o)
	}
	res.Format = f
	if err != nil {
		return res, err
	}
	if len(res.Hosts) == 0 {
		return res, ErrEmpty
	}
	if len(res.Hosts) > MaxHosts {
		res.Hosts = res.Hosts[:MaxHosts]
		res.warn("only the first %d hosts were read", MaxHosts)
	}
	return res, nil
}

// Load reads a file or a folder from disk. A folder is searched for Xshell
// (.xsh) and SecureCRT (.ini) session files; the folder names become tags.
func Load(p string, o Options) (Result, error) {
	st, err := os.Stat(p)
	if err != nil {
		return Result{}, fmt.Errorf("reading %s: %w", p, err)
	}
	if !st.IsDir() {
		if st.Size() > MaxFileSize {
			return Result{}, fmt.Errorf("%w: %s", ErrTooLarge, filepath.Base(p))
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return Result{}, fmt.Errorf("reading %s: %w", p, err)
		}
		return Parse(filepath.Base(p), data, o)
	}
	return ParseFS(os.DirFS(p), o)
}

// ParseFS imports every session file of a folder tree.
func ParseFS(fsys fs.FS, o Options) (Result, error) {
	var res Result
	files, total := 0, 0
	err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil //nolint:nilerr // an unreadable entry is skipped, not fatal
		}
		kind := strings.ToLower(path.Ext(p))
		if kind != ".xsh" && kind != ".ini" {
			return nil
		}
		if files++; files > MaxFiles {
			return fs.SkipAll
		}
		data, rerr := readLimited(fsys, p)
		if rerr != nil {
			res.warn("%s: %v", p, rerr)
			return nil
		}
		if total += len(data); total > MaxTotal {
			res.warn("stopped after %d MB of session files", MaxTotal>>20)
			return fs.SkipAll
		}
		var one Result
		var perr error
		if kind == ".xsh" {
			one, perr = parseXshellSession(p, data, o)
		} else {
			one, perr = parseSecureCRTSession(p, data, o)
		}
		if perr != nil {
			if !errors.Is(perr, errNotSession) {
				res.warn("%s: %v", p, perr)
			}
			return nil
		}
		res.merge(one)
		return nil
	})
	if err != nil {
		return res, fmt.Errorf("reading folder: %w", err)
	}
	if len(res.Hosts) == 0 {
		return res, ErrEmpty
	}
	return res, nil
}

func (r *Result) merge(o Result) {
	r.Hosts = append(r.Hosts, o.Hosts...)
	r.Warnings = append(r.Warnings, o.Warnings...)
	r.Passwords += o.Passwords
	if r.Format == "" {
		r.Format = o.Format
	}
}

func readLimited(fsys fs.FS, p string) ([]byte, error) {
	f, err := fsys.Open(p)
	if err != nil {
		return nil, fmt.Errorf("opening: %w", err)
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, MaxFileSize+1))
	if err != nil {
		return nil, fmt.Errorf("reading: %w", err)
	}
	if len(data) > MaxFileSize {
		return nil, ErrTooLarge
	}
	return data, nil
}

// errNotSession marks a file that is a valid file of the format but not a
// session (SecureCRT's default.ini, folder data).
var errNotSession = errors.New("not a session")

func parseXshellAny(name string, data []byte, o Options) (Result, error) {
	if bytes.HasPrefix(data, []byte("PK\x03\x04")) {
		zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			return Result{}, fmt.Errorf("reading archive: %w", err)
		}
		return ParseFS(zr, o)
	}
	return parseXshellSession(name, data, o)
}

func parseSecureCRTAny(name string, data []byte, o Options) (Result, error) {
	text := strings.TrimSpace(string(bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})))
	if strings.HasPrefix(text, "<") {
		return parseSecureCRTXML(data, o)
	}
	return parseSecureCRTSession(name, data, o)
}

func isUTF16(data []byte) bool {
	return bytes.HasPrefix(data, []byte{0xFF, 0xFE}) || bytes.HasPrefix(data, []byte{0xFE, 0xFF})
}

// decodeText returns data as UTF-8 text: UTF-16 with a byte order mark (what
// Xshell 6 and later write) is converted, a UTF-8 BOM removed.
func decodeText(data []byte) string {
	switch {
	case bytes.HasPrefix(data, []byte{0xFF, 0xFE}), bytes.HasPrefix(data, []byte{0xFE, 0xFF}):
		le := data[0] == 0xFF
		data = data[2:]
		u := make([]uint16, 0, len(data)/2)
		for i := 0; i+1 < len(data); i += 2 {
			if le {
				u = append(u, uint16(data[i])|uint16(data[i+1])<<8)
			} else {
				u = append(u, uint16(data[i+1])|uint16(data[i])<<8)
			}
		}
		return string(utf16.Decode(u))
	default:
		return string(bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF}))
	}
}

// groupTags turns a folder path into tags, one per level.
func groupTags(parts ...string) []string {
	var tags []string
	for _, p := range parts {
		for seg := range strings.SplitSeq(p, "/") {
			if seg = strings.TrimSpace(seg); seg != "" && seg != "." && !slices.Contains(tags, seg) {
				tags = append(tags, seg)
			}
		}
	}
	return tags
}

// sessionName is the file name without its directory and extension.
func sessionName(p string) string {
	base := path.Base(strings.ReplaceAll(p, `\`, "/"))
	return strings.TrimSuffix(base, path.Ext(base))
}

// finishHost fills what a session may lack and checks it can be saved. It
// returns false and a warning when the host cannot be imported.
func finishHost(h model.Host, label string, o Options) (model.Host, string, bool) {
	h.Addr = strings.TrimSpace(h.Addr)
	if h.Addr == "" {
		return h, label + ": no host address, skipped", false
	}
	if strings.TrimSpace(h.User) == "" {
		if o.DefaultUser == "" {
			return h, label + ": no user name, skipped", false
		}
		h.User = o.DefaultUser
	}
	if strings.TrimSpace(h.Name) == "" {
		h.Name = h.Addr
	}
	if h.Port < 0 || h.Port > 65535 {
		return h, fmt.Sprintf("%s: port %d is out of range, skipped", label, h.Port), false
	}
	if h.Port == model.DefaultPort {
		h.Port = 0
	}
	h.ID = model.NewID()
	h.OS = model.OSLinux
	if err := h.Validate(); err != nil {
		return h, label + ": " + err.Error() + ", skipped", false
	}
	return h, "", true
}
