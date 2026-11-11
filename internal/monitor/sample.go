package monitor

import (
	"errors"
	"time"
)

// Sample is one measurement of a machine.
type Sample struct {
	At time.Time
	// Rates is false for the first sample of a series: counters need two
	// readings, so CPU, disk I/O and network speeds are not known yet.
	Rates bool

	// CPU is the busy share of all cores in percent; User and System split it.
	CPU, CPUUser, CPUSystem float64
	// Cores holds the busy share of each core when the system reports it.
	Cores []float64
	// Load is the 1, 5 and 15 minute load average.
	Load [3]float64

	// Memory in bytes. MemUsed excludes what the system can give back
	// (caches); MemCached is that part.
	MemUsed, MemCached, MemTotal uint64

	Disks []Disk
	// DiskRead and DiskWrite are bytes per second over all physical disks.
	// On macOS only the sum is known (iostat does not split it): it is in
	// DiskRead and DiskSplit is false.
	DiskRead, DiskWrite float64
	DiskSplit           bool
	// NetRx and NetTx are bytes per second over all real interfaces.
	NetRx, NetTx float64

	// GPUs is empty when the machine has none or the tools are missing.
	GPUs []GPU

	// Latency is the round trip of the SSH connection, 0 when not measured.
	Latency time.Duration
}

// Disk is a mounted file system.
type Disk struct {
	Mount       string
	Used, Total uint64
}

// Percent is the used share of the disk in percent.
func (d Disk) Percent() float64 { return percent(float64(d.Used), float64(d.Total)) }

// GPU is one graphics card.
type GPU struct {
	Index int
	Name  string
	// Util is the busy share in percent.
	Util float64
	// MemUsed and MemTotal are bytes of video memory.
	MemUsed, MemTotal uint64
	// TempC is the temperature in degrees Celsius, 0 when unknown.
	TempC float64
	// PowerW is the power draw in watts, 0 when unknown.
	PowerW float64
}

// MemPercent is the used share of the video memory in percent.
func (g GPU) MemPercent() float64 { return percent(float64(g.MemUsed), float64(g.MemTotal)) }

// MemPercent is the used share of memory in percent.
func (s Sample) MemPercent() float64 { return percent(float64(s.MemUsed), float64(s.MemTotal)) }

// DiskMax is the fullest disk's used share in percent, 0 without disks.
func (s Sample) DiskMax() float64 {
	m := 0.0
	for _, d := range s.Disks {
		m = max(m, d.Percent())
	}
	return m
}

// GPUUtil is the mean busy share over the GPUs, 0 without any.
func (s Sample) GPUUtil() float64 {
	if len(s.GPUs) == 0 {
		return 0
	}
	sum := 0.0
	for _, g := range s.GPUs {
		sum += g.Util
	}
	return sum / float64(len(s.GPUs))
}

// GPUMem returns the video memory used and in total over all GPUs.
func (s Sample) GPUMem() (used, total uint64) {
	for _, g := range s.GPUs {
		used += g.MemUsed
		total += g.MemTotal
	}
	return used, total
}

// Info is what a machine is, for the "about" card. It changes rarely and is
// read once per connection.
type Info struct {
	// System is a name such as "Ubuntu 22.04.4 LTS".
	System string
	Kernel string
	CPU    string
	// Cores is the number of logical processors.
	Cores int
	// MemTotal is the installed memory in bytes.
	MemTotal uint64
	// Uptime is how long the machine has been running.
	Uptime time.Duration
	// GPUDriver is the display driver and CUDA version, when there is a GPU.
	GPUDriver string
}

// ErrNoData means the command ran but printed nothing usable.
var ErrNoData = errors.New("monitor: the command produced no usable data")

func percent(part, whole float64) float64 {
	if whole <= 0 {
		return 0
	}
	return min(max(part/whole*100, 0), 100)
}
