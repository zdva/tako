package monitor

import (
	"encoding/base64"
	"encoding/json/v2"
	"fmt"
	"strings"
	"time"
	"unicode/utf16"
)

// Windows measures Windows through PowerShell and CIM. It uses the
// "formatted performance data" CIM classes, whose names and properties do not
// depend on the system language (Get-Counter paths do), and returns rates
// directly, so no previous reading is needed.
type Windows struct{}

const windowsScript = `$ProgressPreference='SilentlyContinue';$ErrorActionPreference='SilentlyContinue';` +
	`$os=Get-CimInstance Win32_OperatingSystem;` +
	`$cpu=Get-CimInstance Win32_PerfFormattedData_PerfOS_Processor -Filter "Name='_Total'";` +
	`$mem=Get-CimInstance Win32_PerfFormattedData_PerfOS_Memory;` +
	`$net=@(Get-CimInstance Win32_PerfFormattedData_Tcpip_NetworkInterface);` +
	`$dsk=Get-CimInstance Win32_PerfFormattedData_PerfDisk_PhysicalDisk -Filter "Name='_Total'";` +
	`$ld=@(Get-CimInstance Win32_LogicalDisk -Filter "DriveType=3");` +
	`$smi='';if(Get-Command nvidia-smi){$smi=(nvidia-smi --query-gpu=index,name,utilization.gpu,memory.used,memory.total,temperature.gpu,power.draw --format=csv,noheader,nounits) -join "` + "`n" + `"};` +
	`[pscustomobject]@{` +
	`cpu=[double]$cpu.PercentProcessorTime;user=[double]$cpu.PercentUserTime;priv=[double]$cpu.PercentPrivilegedTime;` +
	`memTotalKB=[double]$os.TotalVisibleMemorySize;memAvailBytes=[double]$mem.AvailableBytes;memCacheBytes=[double]$mem.CacheBytes;` +
	`netRx=[double]($net|Measure-Object BytesReceivedPersec -Sum).Sum;netTx=[double]($net|Measure-Object BytesSentPersec -Sum).Sum;` +
	`diskRead=[double]$dsk.DiskReadBytesPersec;diskWrite=[double]$dsk.DiskWriteBytesPersec;` +
	`disks=@($ld|ForEach-Object{[pscustomobject]@{id=$_.DeviceID;size=[double]$_.Size;free=[double]$_.FreeSpace}});` +
	`nvidia=$smi}|ConvertTo-Json -Compress -Depth 4`

const windowsInfoScript = `$ProgressPreference='SilentlyContinue';$ErrorActionPreference='SilentlyContinue';` +
	`$os=Get-CimInstance Win32_OperatingSystem;$cpu=@(Get-CimInstance Win32_Processor);` +
	`$smi='';if(Get-Command nvidia-smi){$smi=((nvidia-smi --query-gpu=driver_version --format=csv,noheader) -join ' ')};` +
	`[pscustomobject]@{os=$os.Caption;version=$os.Version;build=$os.BuildNumber;cpu=$cpu[0].Name;` +
	`cores=[int]($cpu|Measure-Object NumberOfLogicalProcessors -Sum).Sum;memKB=[double]$os.TotalVisibleMemorySize;` +
	`uptimeSec=[double]((Get-Date)-$os.LastBootUpTime).TotalSeconds;gpuDriver=$smi}|ConvertTo-Json -Compress`

// powershell wraps a script so that it runs under any login shell of the
// OpenSSH server (cmd.exe, PowerShell): the script travels base64-encoded.
func powershell(script string) string {
	u := utf16.Encode([]rune(script))
	b := make([]byte, 0, 2*len(u))
	for _, c := range u {
		b = append(b, byte(c&0xff), byte(c>>8))
	}
	return "powershell -NoProfile -NonInteractive -ExecutionPolicy Bypass -EncodedCommand " + base64.StdEncoding.EncodeToString(b)
}

// Command implements [Collector].
func (Windows) Command() string { return powershell(windowsScript) }

// InfoCommand implements [Collector].
func (Windows) InfoCommand() string { return powershell(windowsInfoScript) }

// jsonObject finds the JSON object in the output of PowerShell, which can be
// surrounded by progress records or a byte order mark.
func jsonObject(out []byte) []byte {
	s := string(out)
	i, j := strings.Index(s, "{"), strings.LastIndex(s, "}")
	if i < 0 || j < i {
		return nil
	}
	return []byte(s[i : j+1])
}

type winSample struct {
	CPU        float64 `json:"cpu"`
	User       float64 `json:"user"`
	Priv       float64 `json:"priv"`
	MemTotalKB float64 `json:"memTotalKB"`
	MemAvail   float64 `json:"memAvailBytes"`
	MemCache   float64 `json:"memCacheBytes"`
	NetRx      float64 `json:"netRx"`
	NetTx      float64 `json:"netTx"`
	DiskRead   float64 `json:"diskRead"`
	DiskWrite  float64 `json:"diskWrite"`
	Disks      any     `json:"disks"`
	Nvidia     string  `json:"nvidia"`
}

// Parse implements [Collector].
func (Windows) Parse(out []byte, _ Raw, at time.Time) (Sample, Raw, error) {
	obj := jsonObject(out)
	if obj == nil {
		return Sample{}, Raw{}, ErrNoData
	}
	var w winSample
	if err := json.Unmarshal(obj, &w); err != nil {
		return Sample{}, Raw{}, fmt.Errorf("reading the PowerShell output: %w", err)
	}
	s := Sample{At: at, Rates: true, DiskSplit: true}
	s.CPU = min(max(w.CPU, 0), 100)
	s.CPUUser = min(max(w.User, 0), 100)
	s.CPUSystem = min(max(w.Priv, 0), 100)
	s.MemTotal = uint64(w.MemTotalKB * 1024)
	if s.MemTotal > 0 {
		avail := min(uint64(w.MemAvail), s.MemTotal)
		s.MemUsed = s.MemTotal - avail
		s.MemCached = min(uint64(w.MemCache), s.MemTotal-s.MemUsed)
	}
	s.NetRx, s.NetTx = max(w.NetRx, 0), max(w.NetTx, 0)
	s.DiskRead, s.DiskWrite = max(w.DiskRead, 0), max(w.DiskWrite, 0)
	s.Disks = winDisks(w.Disks)
	s.GPUs = parseNvidia(w.Nvidia)
	// Windows keeps no load average; Load stays zero.
	return s, Raw{}, nil
}

// winDisks reads the "disks" value, which ConvertTo-Json writes as an array,
// or as a bare object when a version of PowerShell unrolled a single disk.
func winDisks(v any) []Disk {
	var items []any
	switch x := v.(type) {
	case []any:
		items = x
	case map[string]any:
		items = []any{x}
	}
	var out []Disk
	for _, it := range items {
		m, ok := it.(map[string]any)
		if !ok {
			continue
		}
		id, _ := m["id"].(string)
		size, _ := m["size"].(float64)
		free, _ := m["free"].(float64)
		if id == "" || size <= 0 {
			continue
		}
		out = append(out, Disk{Mount: id, Total: uint64(size), Used: uint64(max(size-free, 0))})
	}
	return out
}

type winInfo struct {
	OS        string  `json:"os"`
	Version   string  `json:"version"`
	Build     string  `json:"build"`
	CPU       string  `json:"cpu"`
	Cores     int     `json:"cores"`
	MemKB     float64 `json:"memKB"`
	UptimeSec float64 `json:"uptimeSec"`
	GPUDriver string  `json:"gpuDriver"`
}

// ParseInfo implements [Collector].
func (Windows) ParseInfo(out []byte) (Info, error) {
	obj := jsonObject(out)
	if obj == nil {
		return Info{}, ErrNoData
	}
	var w winInfo
	if err := json.Unmarshal(obj, &w); err != nil {
		return Info{}, fmt.Errorf("reading the PowerShell output: %w", err)
	}
	return Info{
		System:    strings.TrimSpace(w.OS),
		Kernel:    strings.TrimSpace("Windows NT " + w.Version),
		CPU:       strings.TrimSpace(w.CPU),
		Cores:     w.Cores,
		MemTotal:  uint64(w.MemKB * 1024),
		Uptime:    time.Duration(w.UptimeSec * float64(time.Second)),
		GPUDriver: strings.TrimSpace(w.GPUDriver),
	}, nil
}
