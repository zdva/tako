package hostfile

import "strings"

// ini is a parsed INI file: section name (as written) to key to value. Later
// duplicate keys replace earlier ones; sections and keys keep their case, with
// a case-insensitive lookup.
type ini map[string]map[string]string

func parseINI(text string) ini {
	out := ini{}
	section := ""
	for line := range strings.Lines(text) {
		line = strings.TrimSpace(line)
		switch {
		case line == "" || line[0] == ';' || line[0] == '#':
		case line[0] == '[' && strings.HasSuffix(line, "]"):
			section = line[1 : len(line)-1]
			if out[section] == nil {
				out[section] = map[string]string{}
			}
		default:
			k, v, ok := strings.Cut(line, "=")
			if !ok {
				continue
			}
			if out[section] == nil {
				out[section] = map[string]string{}
			}
			out[section][strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	return out
}

// get finds a key in a section, ignoring case.
func (f ini) get(section, key string) string {
	for s, kv := range f {
		if !strings.EqualFold(s, section) {
			continue
		}
		for k, v := range kv {
			if strings.EqualFold(k, key) {
				return v
			}
		}
	}
	return ""
}
