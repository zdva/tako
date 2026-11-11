package scheme

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
)

// MaxITermSize bounds an .itermcolors file; real ones are about 8 KB.
const MaxITermSize = 256 << 10

// ErrITerm means the data is not an iTerm2 color preset.
var ErrITerm = errors.New("scheme: not an iTerm2 color preset")

// ParseITerm reads an iTerm2 color preset (.itermcolors, an XML property
// list). It reads the sixteen "Ansi n Color" entries, the foreground and
// background, and the cursor and selection colors when present. Components
// are taken as sRGB whatever the file's "Color Space" says; iTerm's device
// space differs by a few units at most. The name is the one the caller gives
// (iTerm2 presets do not carry one, the file name is used).
func ParseITerm(data []byte, name string) (Scheme, error) {
	if len(data) > MaxITermSize {
		return Scheme{}, fmt.Errorf("%w: file is larger than %d bytes", ErrITerm, MaxITermSize)
	}
	root, err := parsePlist(data)
	if err != nil {
		return Scheme{}, fmt.Errorf("%w: %w", ErrITerm, err)
	}
	s := Scheme{ID: "custom:" + Slug(name), Name: name}
	if Slug(name) == "" {
		s.ID, s.Name = "custom:imported", "Imported"
	}
	for i := range s.Palette {
		c, ok := plistColor(root, fmt.Sprintf("Ansi %d Color", i))
		if !ok {
			return Scheme{}, fmt.Errorf("%w: missing Ansi %d Color", ErrITerm, i)
		}
		s.Palette[i] = c
	}
	var ok bool
	if s.Foreground, ok = plistColor(root, "Foreground Color"); !ok {
		return Scheme{}, fmt.Errorf("%w: missing Foreground Color", ErrITerm)
	}
	if s.Background, ok = plistColor(root, "Background Color"); !ok {
		return Scheme{}, fmt.Errorf("%w: missing Background Color", ErrITerm)
	}
	s.Cursor, _ = plistColor(root, "Cursor Color")
	s.Selection, _ = plistColor(root, "Selected Text Background Color")
	s.Dark = luminance(s.Background) < 0.5
	return s, nil
}

func luminance(hex string) float64 {
	v, err := strconv.ParseUint(hex[1:], 16, 32)
	if err != nil {
		return 0
	}
	r, g, b := float64(v>>16&0xff), float64(v>>8&0xff), float64(v&0xff)
	return (0.2126*r + 0.7152*g + 0.0722*b) / 255
}

// plistDict is the part of a property list we need: dictionaries of reals
// and dictionaries.
type plistDict struct {
	reals map[string]float64
	dicts map[string]*plistDict
}

func plistColor(root *plistDict, key string) (string, bool) {
	d, ok := root.dicts[key]
	if !ok {
		return "", false
	}
	r, rok := d.reals["Red Component"]
	g, gok := d.reals["Green Component"]
	b, bok := d.reals["Blue Component"]
	if !rok || !gok || !bok {
		return "", false
	}
	return fmt.Sprintf("#%02X%02X%02X", unit(r), unit(g), unit(b)), true
}

func unit(f float64) int {
	if math.IsNaN(f) {
		return 0
	}
	return int(math.Round(min(max(f, 0), 1) * 255))
}

const maxPlistDepth = 8

func parsePlist(data []byte) (*plistDict, error) {
	dec := xml.NewDecoder(bytes.NewReader(data))
	dec.Strict = false
	dec.Entity = xml.HTMLEntity
	for {
		tok, err := dec.Token()
		if err != nil {
			return nil, fmt.Errorf("no <dict>: %w", err)
		}
		if se, ok := tok.(xml.StartElement); ok && se.Name.Local == "dict" {
			return readDict(dec, 1)
		}
	}
}

// readDict reads the children of a <dict> whose start tag was just consumed.
func readDict(dec *xml.Decoder, depth int) (*plistDict, error) {
	if depth > maxPlistDepth {
		return nil, errors.New("dictionaries nest too deeply")
	}
	d := &plistDict{reals: map[string]float64{}, dicts: map[string]*plistDict{}}
	key := ""
	for {
		tok, err := dec.Token()
		if err != nil {
			if errors.Is(err, io.EOF) {
				err = io.ErrUnexpectedEOF
			}
			return nil, fmt.Errorf("reading dict: %w", err)
		}
		switch t := tok.(type) {
		case xml.EndElement:
			return d, nil
		case xml.StartElement:
			switch t.Name.Local {
			case "key":
				key, err = text(dec)
			case "real", "integer":
				var v string
				if v, err = text(dec); err == nil {
					if f, perr := strconv.ParseFloat(v, 64); perr == nil && key != "" {
						d.reals[key] = f
					}
				}
			case "dict":
				var sub *plistDict
				if sub, err = readDict(dec, depth+1); err == nil && key != "" {
					d.dicts[key] = sub
				}
			default:
				err = dec.Skip()
			}
			if err != nil {
				return nil, err
			}
		}
	}
}

func text(dec *xml.Decoder) (string, error) {
	var b bytes.Buffer
	for {
		tok, err := dec.Token()
		if err != nil {
			return "", fmt.Errorf("reading text: %w", err)
		}
		switch t := tok.(type) {
		case xml.CharData:
			b.Write(t)
		case xml.EndElement:
			return b.String(), nil
		}
	}
}
