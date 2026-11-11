package hostfile

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"path"
	"strconv"
	"strings"

	"github.com/wei-dong/tako/internal/model"
)

// scrt is the part of a SecureCRT session the importer reads, whichever file
// it came from.
type scrt struct {
	name, group                 string
	host, user, proto, identity string
	firewall                    string
	port                        int
	hasPassword                 bool
	portSet                     bool
}

func (s scrt) result(label string, o Options) Result {
	res := Result{Format: FormatSecureCRT}
	switch p := strings.ToLower(s.proto); {
	case p == "" || strings.HasPrefix(p, "ssh"):
	default:
		res.warn("%s: %s sessions are not supported, skipped", label, s.proto)
		return res
	}
	h := model.Host{Name: s.name, Addr: s.host, User: s.user, Port: s.port, Tags: groupTags(s.group)}
	if !s.portSet {
		h.Port = 0
	}
	h.Auth = []model.Auth{{Kind: model.AuthPassword}}
	if id := strings.TrimSpace(s.identity); id != "" && !strings.Contains(id, "${") && !strings.Contains(id, "%") {
		h.Auth = []model.Auth{{Kind: model.AuthKey, KeyPath: id}, {Kind: model.AuthPassword}}
	} else if id != "" {
		res.warn("%s: identity file %q depends on SecureCRT's variables; choose the key in Edit host", label, id)
	}
	if s.hasPassword {
		res.Passwords++
	}
	if fw := s.firewall; fw != "" && !strings.EqualFold(fw, "none") {
		res.warn("%s: firewall %q was not imported", label, fw)
	}
	h, msg, ok := finishHost(h, label, o)
	if !ok {
		res.warn("%s", msg)
		return res
	}
	res.Hosts = append(res.Hosts, h)
	return res
}

// dword reads a SecureCRT number: eight hex digits in .ini files and (as
// far as the exports seen show) in XML too, otherwise decimal.
func dword(v string) (int, bool) {
	v = strings.TrimSpace(v)
	if len(v) == 8 {
		if n, err := strconv.ParseUint(v, 16, 32); err == nil {
			return int(min(n, 1<<31-1)), true
		}
	}
	n, err := strconv.Atoi(v)
	return n, err == nil
}

// Value kinds of SecureCRT settings: S: / <string> and D: / <dword>.
const (
	kindString = "S"
	kindDword  = "D"
)

// set records one setting of a session; settings the importer does not use
// are ignored.
func (s *scrt) set(kind, key, val string) {
	switch {
	case kind == kindString && key == "Hostname":
		s.host = val
	case kind == kindString && key == "Username":
		s.user = val
	case kind == kindString && key == "Protocol Name":
		s.proto = val
	case kind == kindString && key == "Identity Filename V2":
		s.identity = val
	case kind == kindString && key == "Firewall Name":
		s.firewall = val
	case kind == kindString && key == "Password" && val != "":
		s.hasPassword = true
	case kind == kindDword && key == "[SSH2] Port":
		s.port, s.portSet = dword(val)
	}
}

// parseSecureCRTSession reads a Sessions/*.ini file: lines of
// S:"name"=text and D:"name"=hex.
func parseSecureCRTSession(p string, data []byte, o Options) (Result, error) {
	base := path.Base(strings.ReplaceAll(p, `\`, "/"))
	if strings.EqualFold(base, "default.ini") || strings.EqualFold(base, "__FolderData__.ini") {
		return Result{}, errNotSession
	}
	s := scrt{name: sessionName(p), group: path.Dir(strings.ReplaceAll(p, `\`, "/"))}
	text := strings.ReplaceAll(decodeText(data), "\x00", "")
	for line := range strings.Lines(text) {
		line = strings.TrimSpace(line)
		kind, rest, ok := strings.Cut(line, `:"`)
		if !ok || len(kind) != 1 {
			continue
		}
		key, val, ok := strings.Cut(rest, `"=`)
		if !ok {
			continue
		}
		s.set(kind, key, val)
	}
	if s.host == "" {
		return Result{}, errNotSession
	}
	return s.result(path.Base(p), o), nil
}

// node is one element of SecureCRT's XML export.
type node struct {
	XMLName xml.Name
	Name    string `xml:"name,attr"`
	Text    string `xml:",chardata"`
	Kids    []node `xml:",any"`
}

const maxXMLDepth = 32

// parseSecureCRTXML reads the file of Tools > Export Settings: nested
// <key name="…"> elements; a key with a Hostname string is a session and the
// keys above it (below "Sessions") are its folders.
func parseSecureCRTXML(data []byte, o Options) (Result, error) {
	dec := xml.NewDecoder(bytes.NewReader(data))
	dec.Strict = false
	var root node
	if err := dec.Decode(&root); err != nil && !errors.Is(err, io.EOF) {
		return Result{}, fmt.Errorf("reading SecureCRT export: %w", err)
	}
	var res Result
	res.Format = FormatSecureCRT
	var walk func(n node, groups []string, depth int)
	walk = func(n node, groups []string, depth int) {
		if depth > maxXMLDepth || len(res.Hosts) >= MaxHosts {
			return
		}
		s := scrt{name: n.Name}
		for _, k := range n.Kids {
			kind := map[string]string{"string": kindString, "dword": kindDword}[k.XMLName.Local]
			s.set(kind, k.Name, strings.TrimSpace(k.Text))
		}
		found := s.host != ""
		if found && n.Name != "Default" {
			s.group = strings.Join(groups, "/")
			one := s.result(n.Name, o)
			res.Hosts = append(res.Hosts, one.Hosts...)
			res.Warnings = append(res.Warnings, one.Warnings...)
			res.Passwords += one.Passwords
			return
		}
		next := groups
		if n.XMLName.Local == "key" && n.Name != "" && n.Name != "Sessions" && depth > 1 {
			next = append(append([]string(nil), groups...), n.Name)
		}
		for _, k := range n.Kids {
			if k.XMLName.Local == "key" {
				walk(k, next, depth+1)
			}
		}
	}
	walk(root, nil, 0)
	return res, nil
}
