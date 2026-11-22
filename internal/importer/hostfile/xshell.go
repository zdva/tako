package hostfile

import (
	"path"
	"strconv"
	"strings"

	"github.com/zdva/tako/internal/model"
)

// parseXshellSession reads one .xsh file. p is its path inside the archive or
// folder (or just its name); the folders above it become tags.
//
// Xshell 5 and later write [CONNECTION] (Host, Port, Protocol=SSH) and
// [CONNECTION:AUTHENTICATION] (UserName, Method, UserKey); Xshell 4 and
// earlier write [General] (Host, Port, UserName, AuthMethod, UserKey,
// Protocol=0 for SSH). Both are read.
func parseXshellSession(p string, data []byte, o Options) (Result, error) {
	res := Result{Format: FormatXshell}
	f := parseINI(decodeText(data))
	label := path.Base(strings.ReplaceAll(p, `\`, "/"))

	conn, auth := "CONNECTION", "CONNECTION:AUTHENTICATION"
	method, key := "Method", "UserKey"
	legacy := false
	if f.get(conn, "Host") == "" && f.get("General", "Host") != "" {
		conn, auth, method, legacy = "General", "General", "AuthMethod", true
	}
	host := f.get(conn, "Host")
	if host == "" {
		return res, errNotSession
	}
	proto := strings.ToLower(f.get(conn, "Protocol"))
	supported := proto == "ssh" || proto == "ssh2" || proto == "" || (legacy && proto == "0")
	if !supported {
		res.warn("%s: %s sessions are not supported, skipped", label, strings.ToUpper(proto))
		return res, nil
	}
	h := model.Host{
		Name: sessionName(p),
		Addr: host,
		User: f.get(auth, "UserName"),
		Note: f.get(conn, "Description"),
		Tags: groupTags(path.Dir(strings.ReplaceAll(p, `\`, "/"))),
	}
	if v := f.get(conn, "Port"); v != "" {
		port, err := strconv.Atoi(v)
		if err != nil {
			res.warn("%s: port %q is not a number, using 22", label, v)
		}
		h.Port = port
	}
	switch f.get(auth, method) {
	case "1":
		h.Auth = []model.Auth{{Kind: model.AuthAgent}, {Kind: model.AuthPassword}}
		if k := f.get(auth, key); k != "" {
			res.warn("%s: key %q is in Xshell's own key store; choose its file in Edit host", label, k)
		}
	case "2":
		h.Auth = []model.Auth{{Kind: model.AuthKeyboardInteractive}}
	default:
		h.Auth = []model.Auth{{Kind: model.AuthPassword}}
	}
	if f.get(auth, "Password") != "" {
		res.Passwords++
	}
	if proxy := f.get("CONNECTION:PROXY", "Proxy"); proxy != "" {
		res.warn("%s: proxy %q was not imported", label, proxy)
	}
	h, msg, ok := finishHost(h, label, o)
	if !ok {
		res.warn("%s", msg)
		return res, nil
	}
	res.Hosts = append(res.Hosts, h)
	return res, nil
}
