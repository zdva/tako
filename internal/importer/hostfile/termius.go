package hostfile

import (
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"

	"github.com/wei-dong/tako/internal/model"
)

func splitHeader(line string) []string {
	r := csv.NewReader(strings.NewReader(strings.TrimSpace(line)))
	r.LazyQuotes = true
	rec, err := r.Read()
	if err != nil {
		return nil
	}
	for i := range rec {
		rec[i] = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(rec[i], "\ufeff")))
	}
	return rec
}

func isHostCol(c string) bool {
	return c == "hostname/ip" || c == "hostname" || c == "host" || c == "address" || c == "ip"
}

// parseTermius reads the CSV Termius imports (Groups, Label, Tags,
// Hostname/IP, Protocol, Port, Username, Password; community exporters add
// SSH_Key). Columns are found by name, in any order. A group path such as
// Production/Web becomes one tag per level, Tags are comma separated. The
// password column is never imported.
func parseTermius(data []byte, o Options) (Result, error) {
	res := Result{Format: FormatTermius}
	r := csv.NewReader(bytes.NewReader(bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})))
	r.LazyQuotes = true
	r.FieldsPerRecord = -1
	head, err := r.Read()
	if err != nil {
		return res, fmt.Errorf("reading Termius CSV header: %w", err)
	}
	col := termiusColumns(head)
	if _, ok := col[colHost]; !ok {
		return res, fmt.Errorf("%w: no Hostname/IP column", ErrUnknownFormat)
	}
	get := func(rec []string, name string) string {
		if i, ok := col[name]; ok && i < len(rec) {
			return strings.TrimSpace(rec[i])
		}
		return ""
	}
	for row := 2; len(res.Hosts) < MaxHosts; row++ {
		rec, err := r.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			res.warn("row %d: %v", row, err)
			continue
		}
		label := fmt.Sprintf("row %d", row)
		if p := strings.ToLower(get(rec, "protocol")); p != "" && p != "ssh" {
			res.warn("%s: %s hosts are not supported, skipped", label, p)
			continue
		}
		h := termiusHost(&res, label, func(name string) string { return get(rec, name) })
		h, msg, ok := finishHost(h, label+" ("+h.Name+")", o)
		if !ok {
			res.warn("%s", msg)
			continue
		}
		res.Hosts = append(res.Hosts, h)
	}
	return res, nil
}

// Termius columns the importer looks for, by their normalized names.
const (
	colHost = "host"
	colKey  = "key"
)

// termiusColumns maps normalized column names to their index; the first of
// duplicated columns wins.
func termiusColumns(head []string) map[string]int {
	col := map[string]int{}
	for i, c := range head {
		c = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(c, "\ufeff")))
		if isHostCol(c) {
			c = colHost
		}
		if c == "ssh_key" || c == "ssh key" || c == colKey {
			c = colKey
		}
		if _, dup := col[c]; !dup {
			col[c] = i
		}
	}
	return col
}

// termiusHost builds the host of one row, counting a saved password and
// warning about a key (Termius keeps keys in its own store).
func termiusHost(res *Result, label string, get func(string) string) model.Host {
	h := model.Host{
		Name: get("label"), Addr: get(colHost), User: get("username"),
		Auth: []model.Auth{{Kind: model.AuthPassword}},
		Tags: groupTags(get("groups")),
	}
	for t := range strings.SplitSeq(get("tags"), ",") {
		if t = strings.TrimSpace(t); t != "" && !slices.Contains(h.Tags, t) {
			h.Tags = append(h.Tags, t)
		}
	}
	if v := get("port"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			res.warn("%s: port %q is not a number, using 22", label, v)
		}
		h.Port = n
	}
	if get("password") != "" {
		res.Passwords++
	}
	if k := get(colKey); k != "" {
		res.warn("%s: key %q is not a file; choose it in Edit host", label, k)
		h.Auth = []model.Auth{{Kind: model.AuthAgent}, {Kind: model.AuthPassword}}
	}
	if h.Name == "" {
		h.Name = h.Addr
	}
	return h
}
