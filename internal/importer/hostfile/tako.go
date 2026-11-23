package hostfile

import (
	"encoding/json/v2"
	"fmt"

	"github.com/zdva/tako/internal/model"
)

// export is the document hostlib.Library.Export writes: hosts.json without
// secret references.
type export struct {
	Version int          `json:"version"`
	Hosts   []model.Host `json:"hosts"`
	Tags    []model.Tag  `json:"tags"`
}

// parseTako reads a Tako export. Host IDs are kept so jump chains still
// point at the right hosts; the library remaps them on import (AddMany).
// Secret references are dropped even if a hand-edited file carries them.
func parseTako(data []byte) (Result, error) {
	var doc export
	if err := json.Unmarshal(data, &doc); err != nil {
		return Result{}, fmt.Errorf("reading Tako export: %w", err)
	}
	res := Result{Format: FormatTako, Tags: doc.Tags}
	for _, h := range doc.Hosts {
		for i := range h.Auth {
			h.Auth[i].SecretRef = ""
		}
		if h.Proxy != nil {
			h.Proxy.SecretRef = ""
		}
		if err := h.Validate(); err != nil {
			res.warn("%s: %v, skipped", h.Name, err)
			continue
		}
		res.Hosts = append(res.Hosts, h)
	}
	return res, nil
}
