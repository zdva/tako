package monitor

import (
	"bufio"
	"strconv"
	"strings"
	"time"
)

// Linux measures Linux (and, with less detail, other systems with /proc).
type Linux struct{}

// linuxScript prints each source under a "##NAME" header. Every command has a
// fallback: a missing tool leaves its section empty instead of failing the
// sample.
const linuxScript = `export LC_ALL=C; ` +
	`echo "##STAT"; grep "^cpu" /proc/stat; ` +
	`echo "##LOAD"; cat /proc/loadavg; ` +
	`echo "##MEM"; grep -E "^(MemTotal|MemFree|MemAvailable|Buffers|Cached|SReclaimable|Shmem):" /proc/meminfo; ` +
	`echo "##DISKSTATS"; cat /proc/diskstats; ` +
	`echo "##NET"; cat /proc/net/dev; ` +
	`echo "##DF"; df -P -k 2>/dev/null; ` +
	`if command -v nvidia-smi >/dev/null 2>&1; then echo "##NVIDIA"; ` +
	`nvidia-smi --query-gpu=index,name,utilization.gpu,memory.used,memory.total,temperature.gpu,power.draw --format=csv,noheader,nounits 2>/dev/null; ` +
	`elif command -v rocm-smi >/dev/null 2>&1; then echo "##ROCM"; ` +
	`rocm-smi --showuse --showmeminfo vram --showtemp --showproductname --json 2>/dev/null; fi`

const linuxInfoScript = `export LC_ALL=C; ` +
	`echo "##OS"; grep -m1 "^PRETTY_NAME=" /etc/os-release 2>/dev/null; ` +
	`echo "##KERNEL"; uname -sr; ` +
	`echo "##CPU"; grep -m1 "model name" /proc/cpuinfo 2>/dev/null; grep -c "^processor" /proc/cpuinfo 2>/dev/null; ` +
	`echo "##MEM"; grep -m1 "^MemTotal:" /proc/meminfo; ` +
	`echo "##UPTIME"; cat /proc/uptime; ` +
	`echo "##GPU"; if command -v nvidia-smi >/dev/null 2>&1; then ` +
	`nvidia-smi --query-gpu=driver_version --format=csv,noheader 2>/dev/null | head -n 1; ` +
	`nvidia-smi 2>/dev/null | grep -o "CUDA Version: [0-9.]*" | head -n 1; fi`

// Command implements [Collector].
func (Linux) Command() string { return "sh -c '" + linuxScript + "'" }

// InfoCommand implements [Collector].
func (Linux) InfoCommand() string { return "sh -c '" + linuxInfoScript + "'" }

// cpuTimes are the jiffies of one /proc/stat "cpu" line.
type cpuTimes struct {
	user, system, idle, total uint64
}

func parseCPULine(fields []string) (cpuTimes, bool) {
	if len(fields) < 5 {
		return cpuTimes{}, false
	}
	var v [10]uint64
	for i := 1; i < len(fields) && i <= 10; i++ {
		n, err := strconv.ParseUint(fields[i], 10, 64)
		if err != nil {
			return cpuTimes{}, false
		}
		v[i-1] = n
	}
	// user nice system idle iowait irq softirq steal (guest is inside user)
	user, nice, system, idle, iowait, irq, softirq, steal := v[0], v[1], v[2], v[3], v[4], v[5], v[6], v[7]
	return cpuTimes{
		user:   user + nice,
		system: system + irq + softirq,
		idle:   idle + iowait,
		total:  user + nice + system + idle + iowait + irq + softirq + steal,
	}, true
}

// share returns the busy, user and system percentages between two readings.
func (c cpuTimes) share(prev cpuTimes) (busy, user, system float64, ok bool) {
	if c.total <= prev.total || c.idle < prev.idle || c.user < prev.user || c.system < prev.system {
		return 0, 0, 0, false
	}
	total := float64(c.total - prev.total)
	idle := float64(c.idle - prev.idle)
	return percent(total-idle, total), percent(float64(c.user-prev.user), total), percent(float64(c.system-prev.system), total), true
}

// Parse implements [Collector].
func (Linux) Parse(out []byte, prev Raw, at time.Time) (Sample, Raw, error) {
	sec := sections(out)
	if sec["STAT"] == "" && sec["MEM"] == "" {
		return Sample{}, Raw{}, ErrNoData
	}
	s := Sample{At: at}
	raw := Raw{valid: true, at: at.UnixNano()}

	var cores []cpuTimes
	for line := range strings.Lines(sec["STAT"]) {
		f := strings.Fields(line)
		if len(f) == 0 || !strings.HasPrefix(f[0], "cpu") {
			continue
		}
		t, ok := parseCPULine(f)
		if !ok {
			continue
		}
		if f[0] == "cpu" {
			raw.cpu = t
		} else {
			cores = append(cores, t)
		}
	}
	raw.cores = cores

	f := strings.Fields(sec["LOAD"])
	for i := range min(3, len(f)) {
		s.Load[i], _ = strconv.ParseFloat(f[i], 64)
	}

	mem := parseMeminfo(sec["MEM"])
	s.MemTotal = mem["MemTotal"]
	avail, hasAvail := mem["MemAvailable"]
	if !hasAvail {
		avail = mem["MemFree"] + mem["Buffers"] + mem["Cached"] + mem["SReclaimable"] - mem["Shmem"]
	}
	if s.MemTotal > 0 {
		s.MemUsed = s.MemTotal - min(avail, s.MemTotal)
		s.MemCached = min(mem["Buffers"]+mem["Cached"]+mem["SReclaimable"]-min(mem["Shmem"], mem["Cached"]), s.MemTotal)
	}

	raw.disk[0], raw.disk[1] = parseDiskstats(sec["DISKSTATS"])
	raw.net[0], raw.net[1] = parseNetDev(sec["NET"])
	s.Disks = parseDF(sec["DF"])
	s.GPUs = parseNvidia(sec["NVIDIA"])
	if len(s.GPUs) == 0 {
		s.GPUs = parseROCm(sec["ROCM"])
	}

	if prev.valid {
		dt := time.Duration(raw.at - prev.at).Seconds()
		if busy, user, system, ok := raw.cpu.share(prev.cpu); ok {
			s.Rates = true
			s.CPU, s.CPUUser, s.CPUSystem = busy, user, system
		}
		if len(raw.cores) == len(prev.cores) {
			for i := range raw.cores {
				busy, _, _, _ := raw.cores[i].share(prev.cores[i])
				s.Cores = append(s.Cores, busy)
			}
		}
		if dt > 0 {
			s.DiskSplit = true
			s.DiskRead = rate(raw.disk[0], prev.disk[0], dt)
			s.DiskWrite = rate(raw.disk[1], prev.disk[1], dt)
			s.NetRx = rate(raw.net[0], prev.net[0], dt)
			s.NetTx = rate(raw.net[1], prev.net[1], dt)
		}
	}
	return s, raw, nil
}

// rate is the speed of a counter; a counter that went back (a reboot, a
// removed interface) gives 0.
func rate(now, before uint64, seconds float64) float64 {
	if now < before || seconds <= 0 {
		return 0
	}
	return float64(now-before) / seconds
}

// parseMeminfo returns /proc/meminfo values in bytes.
func parseMeminfo(s string) map[string]uint64 {
	m := map[string]uint64{}
	for line := range strings.Lines(s) {
		name, rest, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		f := strings.Fields(rest)
		if len(f) == 0 {
			continue
		}
		n, err := strconv.ParseUint(f[0], 10, 64)
		if err != nil {
			continue
		}
		if len(f) > 1 && strings.EqualFold(f[1], "kB") {
			n *= 1024
		}
		m[strings.TrimSpace(name)] = n
	}
	return m
}

// isPhysicalDisk tells whole disks from partitions, loop and RAM devices and
// device-mapper volumes (counted twice otherwise).
func isPhysicalDisk(name string) bool {
	for _, p := range []string{"sd", "vd", "xvd", "hd"} {
		if rest, ok := strings.CutPrefix(name, p); ok {
			return rest != "" && !strings.ContainsAny(rest, "0123456789")
		}
	}
	if rest, ok := strings.CutPrefix(name, "nvme"); ok {
		return strings.Contains(rest, "n") && !strings.Contains(rest, "p")
	}
	if rest, ok := strings.CutPrefix(name, "mmcblk"); ok {
		return rest != "" && !strings.Contains(rest, "p")
	}
	return false
}

func parseDiskstats(s string) (read, written uint64) {
	for line := range strings.Lines(s) {
		f := strings.Fields(line)
		if len(f) < 10 || !isPhysicalDisk(f[2]) {
			continue
		}
		r, err1 := strconv.ParseUint(f[5], 10, 64)
		w, err2 := strconv.ParseUint(f[9], 10, 64)
		if err1 != nil || err2 != nil {
			continue
		}
		read += r * 512
		written += w * 512
	}
	return read, written
}

// isRealInterface skips loopback and virtual interfaces, whose traffic is
// either local or counted again on the physical one.
func isRealInterface(name string) bool {
	if name == "lo" {
		return false
	}
	for _, p := range []string{"veth", "docker", "br-", "virbr", "cali", "flannel", "cni", "kube", "tun", "tap", "lxc"} {
		if strings.HasPrefix(name, p) {
			return false
		}
	}
	return true
}

func parseNetDev(s string) (rx, tx uint64) {
	for line := range strings.Lines(s) {
		name, rest, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		if !isRealInterface(strings.TrimSpace(name)) {
			continue
		}
		f := strings.Fields(rest)
		if len(f) < 9 {
			continue
		}
		r, err1 := strconv.ParseUint(f[0], 10, 64)
		t, err2 := strconv.ParseUint(f[8], 10, 64)
		if err1 != nil || err2 != nil {
			continue
		}
		rx += r
		tx += t
	}
	return rx, tx
}

// parseDF reads "df -P -k": one disk per device, real file systems only.
func parseDF(s string) []Disk { return parseDFWith(s, false) }

// parseDFWith is parseDF; dataVolume keeps macOS's "/System/Volumes/Data",
// where the user's files are, instead of dropping the system volumes.
func parseDFWith(s string, dataVolume bool) []Disk {
	var out []Disk
	seen := map[string]int{}
	sc := bufio.NewScanner(strings.NewReader(s))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) < 6 || f[0] == "Filesystem" {
			continue
		}
		dev, mount := f[0], strings.Join(f[5:], " ")
		if !isRealMount(dev, mount, dataVolume) {
			continue
		}
		total, err1 := strconv.ParseUint(f[1], 10, 64)
		used, err2 := strconv.ParseUint(f[2], 10, 64)
		if err1 != nil || err2 != nil || total == 0 {
			continue
		}
		d := Disk{Mount: mount, Used: used * 1024, Total: total * 1024}
		// A device mounted twice (a bind mount) is listed at its shortest path.
		if i, dup := seen[dev]; dup {
			if len(mount) < len(out[i].Mount) {
				out[i] = d
			}
			continue
		}
		seen[dev] = len(out)
		out = append(out, d)
	}
	return out
}

func isRealMount(dev, mount string, dataVolume bool) bool {
	if dataVolume && mount == "/System/Volumes/Data" {
		return true
	}
	switch {
	case strings.HasPrefix(dev, "/dev/loop"):
		return false
	case strings.HasPrefix(mount, "/snap/"), strings.HasPrefix(mount, "/var/lib/docker/"),
		strings.HasPrefix(mount, "/run"), strings.HasPrefix(mount, "/sys"), strings.HasPrefix(mount, "/proc"),
		strings.HasPrefix(mount, "/dev"), strings.HasPrefix(mount, "/System/Volumes/"):
		return false
	}
	return strings.HasPrefix(dev, "/") || strings.Contains(dev, ":") // devices, NFS and SMB
}

// ParseInfo implements [Collector].
func (Linux) ParseInfo(out []byte) (Info, error) {
	sec := sections(out)
	var in Info
	if v, ok := strings.CutPrefix(strings.TrimSpace(sec["OS"]), "PRETTY_NAME="); ok {
		in.System = strings.Trim(v, `"'`)
	}
	in.Kernel = strings.TrimSpace(sec["KERNEL"])
	cpuLines := strings.Split(strings.TrimSpace(sec["CPU"]), "\n")
	if len(cpuLines) > 0 {
		if _, v, ok := strings.Cut(cpuLines[0], ":"); ok {
			in.CPU = strings.Join(strings.Fields(v), " ")
		}
	}
	if len(cpuLines) > 1 {
		in.Cores, _ = strconv.Atoi(strings.TrimSpace(cpuLines[len(cpuLines)-1]))
	}
	in.MemTotal = parseMeminfo(sec["MEM"])["MemTotal"]
	if f := strings.Fields(sec["UPTIME"]); len(f) > 0 {
		if secs, err := strconv.ParseFloat(f[0], 64); err == nil {
			in.Uptime = time.Duration(secs * float64(time.Second))
		}
	}
	var gpu []string
	for line := range strings.Lines(sec["GPU"]) {
		if l := strings.TrimSpace(line); l != "" {
			gpu = append(gpu, l)
		}
	}
	switch len(gpu) {
	case 0:
	case 1:
		in.GPUDriver = gpu[0]
	default:
		in.GPUDriver = gpu[0] + " · " + gpu[1]
	}
	if in.System == "" && in.Kernel == "" && in.CPU == "" {
		return Info{}, ErrNoData
	}
	return in, nil
}
