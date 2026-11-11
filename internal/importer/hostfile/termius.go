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
	col := map[string]int{}
	for i, c := range head {
		c = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(c, "\ufeff")))
		if isHostCol(c) {
			c = "host"
		}
		if c == "ssh_key" || c == "ssh key" || c == "key" {
			c = "key"
		}
		if _, dup := col[c]; !dup {
			col[c] = i
		}
	}
	if _, ok := col["host"]; !ok {
		return res, fmt.Errorf("%w: no Hostname/IP column", ErrUnknownFormat)
	}
	get := func(rec []string, name string) string {
		if i, ok := col[name]; ok && i < len(rec) {
			return strings.TrimSpace(rec[i])
		}
		return ""
	}
	for row := 2; ; row++ {
		rec, err := r.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			res.warn("row %d: %v", row, err)
			continue
		}
		if len(res.Hosts) >= MaxHosts {
			break
		}
		label := fmt.Sprintf("row %d", row)
		if p := strings.ToLower(get(rec, "protocol")); p != "" && p != "ssh" {
			res.warn("%s: %s hosts are not supported, skipped", label, p)
			continue
		}
		h := model.Host{
			Name: get(rec, "label"), Addr: get(rec, "host"), User: get(rec, "username"),
			Auth: []model.Auth{{Kind: model.AuthPassword}},
			Tags: groupTags(get(rec, "groups")),
		}
		for t := range strings.SplitSeq(get(rec, "tags"), ",") {
			if t = strings.TrimSpace(t); t != "" && !slices.Contains(h.Tags, t) {
				h.Tags = append(h.Tags, t)
			}
		}
		if v := get(rec, "port"); v != "" {
			n, perr := strconv.Atoi(v)
			if perr != nil {
				res.warn("%s: port %q is not a number, using 22", label, v)
			}
			h.Port = n
		}
		if get(rec, "password") != "" {
			res.Passwords++
		}
		if k := get(rec, "key"); k != "" {
			res.warn("%s: key %q is not a file; choose it in Edit host", label, k)
			h.Auth = []model.Auth{{Kind: model.AuthAgent}, {Kind: model.AuthPassword}}
		}
		if h.Name == "" {
			h.Name = h.Addr
		}
		h, msg, ok := finishHost(h, label+" ("+h.Name+")", o)
		if !ok {
			res.warn("%s", msg)
			continue
		}
		res.Hosts = append(res.Hosts, h)
	}
	return res, nil
}
