package monitor

import (
	"strconv"
	"strings"
	"time"
)

// Darwin measures macOS with top, vm_stat, iostat and netstat.
type Darwin struct{}

// darwinScript: top takes two readings a second apart, because its first CPU
// line is an average since an unspecified moment; the second is the accurate
// one. top sleeps in between, so the machine does no work meanwhile.
const darwinScript = `export LC_ALL=C; ` +
	`echo "##TOP"; top -l 2 -n 0 -s 1 | grep -E "^(Load Avg|CPU usage)"; ` +
	`echo "##MEMSIZE"; sysctl -n hw.memsize; ` +
	`echo "##VM"; vm_stat; ` +
	`echo "##IO"; iostat -Id; ` +
	`echo "##NET"; netstat -ib; ` +
	`echo "##DF"; df -P -k 2>/dev/null; ` +
	`if command -v nvidia-smi >/dev/null 2>&1; then echo "##NVIDIA"; ` +
	`nvidia-smi --query-gpu=index,name,utilization.gpu,memory.used,memory.total,temperature.gpu,power.draw --format=csv,noheader,nounits 2>/dev/null; fi`

const darwinInfoScript = `export LC_ALL=C; ` +
	`echo "##OS"; sw_vers -productName; sw_vers -productVersion; ` +
	`echo "##KERNEL"; uname -sr; ` +
	`echo "##CPU"; sysctl -n machdep.cpu.brand_string; sysctl -n hw.logicalcpu; ` +
	`echo "##MEMSIZE"; sysctl -n hw.memsize; ` +
	`echo "##BOOT"; sysctl -n kern.boottime; date +%s`

// Command implements [Collector].
func (Darwin) Command() string { return "sh -c '" + darwinScript + "'" }

// InfoCommand implements [Collector].
func (Darwin) InfoCommand() string { return "sh -c '" + darwinInfoScript + "'" }

// Parse implements [Collector].
func (Darwin) Parse(out []byte, prev Raw, at time.Time) (Sample, Raw, error) {
	sec := sections(out)
	if sec["TOP"] == "" && sec["VM"] == "" {
		return Sample{}, Raw{}, ErrNoData
	}
	s := Sample{At: at}
	raw := Raw{valid: true, at: at.UnixNano()}

	// The last CPU line is the accurate one.
	for line := range strings.Lines(sec["TOP"]) {
		switch {
		case strings.HasPrefix(line, "Load Avg:"):
			for i, f := range strings.Split(strings.TrimPrefix(line, "Load Avg:"), ",") {
				if i < 3 {
					s.Load[i], _ = strconv.ParseFloat(strings.TrimSpace(f), 64)
				}
			}
		case strings.HasPrefix(line, "CPU usage:"):
			user, sys, idle, ok := parseTopCPU(line)
			if ok {
				s.CPUUser, s.CPUSystem = user, sys
				s.CPU = min(max(100-idle, 0), 100)
				s.Rates = true // top measures over its own interval
			}
		}
	}

	if f := strings.Fields(sec["MEMSIZE"]); len(f) > 0 {
		s.MemTotal, _ = strconv.ParseUint(f[0], 10, 64)
	}
	s.MemUsed, s.MemCached = parseVMStat(sec["VM"], s.MemTotal)

	raw.disk[0] = parseIostatTotal(sec["IO"])
	rx, tx := parseNetstat(sec["NET"])
	raw.net[0], raw.net[1] = rx, tx
	s.Disks = parseDFWith(sec["DF"], true)
	s.GPUs = parseNvidia(sec["NVIDIA"])

	if prev.valid {
		if dt := time.Duration(raw.at - prev.at).Seconds(); dt > 0 {
			// iostat cannot tell reads from writes: the total goes in DiskRead.
			s.DiskRead = rate(raw.disk[0], prev.disk[0], dt)
			s.NetRx = rate(raw.net[0], prev.net[0], dt)
			s.NetTx = rate(raw.net[1], prev.net[1], dt)
		}
	}
	return s, raw, nil
}

// parseTopCPU reads "CPU usage: 5.26% user, 10.52% sys, 84.21% idle".
func parseTopCPU(line string) (user, sys, idle float64, ok bool) {
	_, rest, _ := strings.Cut(line, ":")
	found := 0
	for part := range strings.SplitSeq(rest, ",") {
		f := strings.Fields(part)
		if len(f) < 2 {
			continue
		}
		v, err := strconv.ParseFloat(strings.TrimSuffix(f[0], "%"), 64)
		if err != nil {
			continue
		}
		switch f[1] {
		case "user":
			user = v
			found++
		case "sys":
			sys = v
			found++
		case "idle":
			idle = v
			found++
		}
	}
	return user, sys, idle, found == 3
}

// parseVMStat computes what Activity Monitor calls "Memory Used" (app memory
// plus wired plus compressed) and "Cached Files" (file-backed plus
// purgeable), in bytes.
func parseVMStat(s string, total uint64) (used, cached uint64) {
	var page uint64 = 4096
	pages := map[string]uint64{}
	for line := range strings.Lines(s) {
		if strings.HasPrefix(line, "Mach Virtual Memory Statistics") {
			if _, rest, ok := strings.Cut(line, "page size of "); ok {
				if f := strings.Fields(rest); len(f) > 0 {
					if n, err := strconv.ParseUint(f[0], 10, 64); err == nil && n > 0 {
						page = n
					}
				}
			}
			continue
		}
		name, val, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		n, err := strconv.ParseUint(strings.TrimSuffix(strings.TrimSpace(val), "."), 10, 64)
		if err != nil {
			continue
		}
		pages[strings.Trim(strings.TrimSpace(name), `"`)] = n
	}
	app := pages["Anonymous pages"]
	purgeable := pages["Pages purgeable"]
	if app >= purgeable {
		app -= purgeable
	}
	used = (app + pages["Pages wired down"] + pages["Pages occupied by compressor"]) * page
	cached = (pages["File-backed pages"] + purgeable) * page
	if total > 0 {
		used = min(used, total)
		cached = min(cached, total-used)
	}
	return used, cached
}

// parseIostatTotal sums the MB column of "iostat -Id" over all disks, in
// bytes. The header names the disks on its first line and has three columns
// (KB/t, xfrs, MB) for each.
func parseIostatTotal(s string) uint64 {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) < 3 {
		return 0
	}
	disks := len(strings.Fields(lines[0]))
	vals := strings.Fields(lines[len(lines)-1])
	var total float64
	for i := range disks {
		if 3*i+2 >= len(vals) {
			break
		}
		mb, err := strconv.ParseFloat(vals[3*i+2], 64)
		if err == nil {
			total += mb
		}
	}
	return uint64(total * 1024 * 1024)
}

// parseNetstat sums the byte counters of the physical interfaces in
// "netstat -ib" (the <Link#> rows; the others repeat them per address).
func parseNetstat(s string) (rx, tx uint64) {
	for line := range strings.Lines(s) {
		f := strings.Fields(line)
		if len(f) < 8 || !strings.HasPrefix(f[2], "<Link#") || !isDarwinInterface(f[0]) {
			continue
		}
		n := len(f)
		r, err1 := strconv.ParseUint(f[n-5], 10, 64)
		t, err2 := strconv.ParseUint(f[n-2], 10, 64)
		if err1 != nil || err2 != nil {
			continue
		}
		rx += r
		tx += t
	}
	return rx, tx
}

func isDarwinInterface(name string) bool {
	for _, p := range []string{"en", "pdp_ip", "ppp", "bond", "wl"} {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

// ParseInfo implements [Collector].
func (Darwin) ParseInfo(out []byte) (Info, error) {
	sec := sections(out)
	var in Info
	if f := strings.Split(strings.TrimSpace(sec["OS"]), "\n"); len(f) >= 2 {
		in.System = strings.TrimSpace(f[0]) + " " + strings.TrimSpace(f[1])
	}
	in.Kernel = strings.TrimSpace(sec["KERNEL"])
	if f := strings.Split(strings.TrimSpace(sec["CPU"]), "\n"); len(f) >= 1 {
		in.CPU = strings.TrimSpace(f[0])
		if len(f) >= 2 {
			in.Cores, _ = strconv.Atoi(strings.TrimSpace(f[1]))
		}
	}
	if f := strings.Fields(sec["MEMSIZE"]); len(f) > 0 {
		in.MemTotal, _ = strconv.ParseUint(f[0], 10, 64)
	}
	// "{ sec = 1700000000, usec = 0 } Tue Nov 14 ..." then the current time.
	boot := strings.TrimSpace(sec["BOOT"])
	if _, after, ok := strings.Cut(boot, "sec = "); ok {
		secStr, _, _ := strings.Cut(after, ",")
		started, err1 := strconv.ParseInt(strings.TrimSpace(secStr), 10, 64)
		lines := strings.Split(boot, "\n")
		now, err2 := strconv.ParseInt(strings.TrimSpace(lines[len(lines)-1]), 10, 64)
		if err1 == nil && err2 == nil && now > started {
			in.Uptime = time.Duration(now-started) * time.Second
		}
	}
	if in.System == "" && in.CPU == "" {
		return Info{}, ErrNoData
	}
	return in, nil
}
