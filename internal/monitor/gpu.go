package monitor

import (
	"encoding/json/v2"
	"slices"
	"strconv"
	"strings"
)

const mib = 1 << 20

// parseNvidia reads
// "nvidia-smi --query-gpu=index,name,utilization.gpu,memory.used,memory.total,temperature.gpu,power.draw --format=csv,noheader,nounits".
// Memory is in MiB. A field the card does not report is "[N/A]" and becomes 0.
func parseNvidia(s string) []GPU {
	var out []GPU
	for line := range strings.Lines(s) {
		f := strings.Split(strings.TrimSpace(line), ", ")
		if len(f) < 7 {
			continue
		}
		idx, err := strconv.Atoi(strings.TrimSpace(f[0]))
		if err != nil {
			continue
		}
		g := GPU{Index: idx, Name: strings.TrimSpace(f[1])}
		g.Util = smiNumber(f[2])
		g.MemUsed = uint64(smiNumber(f[3]) * mib)
		g.MemTotal = uint64(smiNumber(f[4]) * mib)
		g.TempC = smiNumber(f[5])
		g.PowerW = smiNumber(f[6])
		out = append(out, g)
	}
	return out
}

func smiNumber(s string) float64 {
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil || v < 0 {
		return 0
	}
	return v
}

// parseROCm reads "rocm-smi --json": one object per card ("card0", ...), with
// values as strings under descriptive keys that vary a little between
// versions, so keys are matched by their words.
func parseROCm(s string) []GPU {
	var cards map[string]map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(s)), &cards); err != nil {
		return nil
	}
	names := make([]string, 0, len(cards))
	for k := range cards {
		if strings.HasPrefix(k, "card") {
			names = append(names, k)
		}
	}
	slices.SortFunc(names, func(a, b string) int { return cardIndex(a) - cardIndex(b) })
	var out []GPU
	for _, name := range names {
		c := cards[name]
		g := GPU{Index: cardIndex(name)}
		for k, v := range c {
			lk := strings.ToLower(k)
			val := anyNumber(v)
			switch {
			case strings.Contains(lk, "gpu use"):
				g.Util = val
			case strings.Contains(lk, "vram total used"), strings.Contains(lk, "vram used"):
				g.MemUsed = uint64(val)
			case strings.Contains(lk, "vram total memory"):
				g.MemTotal = uint64(val)
			case strings.Contains(lk, "temperature") && (strings.Contains(lk, "edge") || g.TempC == 0):
				g.TempC = val
			case strings.Contains(lk, "card series"), strings.Contains(lk, "card model"):
				if str, ok := v.(string); ok && g.Name == "" {
					g.Name = str
				}
			}
		}
		out = append(out, g)
	}
	return out
}

func cardIndex(name string) int {
	n, _ := strconv.Atoi(strings.TrimPrefix(name, "card"))
	return n
}

func anyNumber(v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case string:
		return smiNumber(x)
	}
	return 0
}
