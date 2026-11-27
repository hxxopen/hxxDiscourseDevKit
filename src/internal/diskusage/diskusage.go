// Package diskusage reports how much space DevKit and its WSL distribution
// are using. A WSL2 virtual disk only grows, so the report includes both the
// ext4 filesystem usage and the size of the ext4.vhdx file that backs it.
package diskusage

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/hxx/hxxDiscourseDevKit/internal/config"
	"github.com/hxx/hxxDiscourseDevKit/internal/runtime"
)

const wslCommandTimeout = 10 * time.Second

// Volume is one filesystem or virtual disk shown by the dashboard.
type Volume struct {
	ID          string  `json:"id"`
	Kind        string  `json:"kind"`
	Label       string  `json:"label"`
	Path        string  `json:"path"`
	TotalBytes  uint64  `json:"total_bytes"`
	UsedBytes   uint64  `json:"used_bytes"`
	FreeBytes   uint64  `json:"free_bytes"`
	PercentUsed float64 `json:"percent_used"`
	VHDXPath    string  `json:"vhdx_path,omitempty"`
	VHDXBytes   uint64  `json:"vhdx_bytes,omitempty"`
	Available   bool    `json:"available"`
	Error       string  `json:"error,omitempty"`
}

// Report is the dashboard payload for disk usage.
type Report struct {
	GeneratedAt time.Time `json:"generated_at"`
	Volumes     []Volume  `json:"volumes"`
}

// Provider collects disk usage for the running instance.
type Provider struct {
	rt    *runtime.Manager
	paths *config.Paths
	now   func() time.Time
}

// New creates a disk usage provider.
func New(rt *runtime.Manager, paths *config.Paths) *Provider {
	return &Provider{rt: rt, paths: paths, now: time.Now}
}

// Report returns the WSL virtual disk plus the host volumes that matter to
// the instance. Partial failures are reported per volume instead of failing
// the whole payload.
func (p *Provider) Report(ctx context.Context) Report {
	volumes := make([]Volume, 0, 3)
	wslVolume, vhdxPath := p.wslVolume(ctx)
	volumes = append(volumes, wslVolume)
	volumes = append(volumes, p.hostVolumes(vhdxPath)...)
	return Report{GeneratedAt: p.now().UTC(), Volumes: volumes}
}

func (p *Provider) wslVolume(ctx context.Context) (Volume, string) {
	volume := Volume{ID: "wsl", Kind: "wsl", Label: "WSL 虚拟磁盘"}
	if p.rt == nil {
		volume.Error = "运行时管理器不可用"
		return volume, ""
	}
	status := p.rt.WSLStatus(ctx)
	if !status.Available {
		volume.Error = status.Error
		if volume.Error == "" {
			volume.Error = "WSL2 不可用"
		}
		return volume, ""
	}
	volume.Label = "WSL 虚拟磁盘（" + status.Distro + "）"

	linuxPath := p.wslDataPath(status)
	volume.Path = linuxPath
	total, used, free, err := p.wslDF(ctx, status.Distro, linuxPath)
	if err != nil && linuxPath != "/" {
		// The configured data root may not exist yet. Fall back to the root
		// filesystem so the card still shows the ext4 image usage.
		linuxPath = "/"
		volume.Path = linuxPath
		total, used, free, err = p.wslDF(ctx, status.Distro, linuxPath)
	}
	if err != nil {
		volume.Error = err.Error()
	} else {
		volume.TotalBytes = total
		volume.UsedBytes = used
		volume.FreeBytes = free
		volume.PercentUsed = percent(used, total)
		volume.Available = true
	}

	if vhdxPath, vhdxBytes := distroVHDX(status.Distro); vhdxPath != "" {
		volume.VHDXPath = vhdxPath
		volume.VHDXBytes = vhdxBytes
		return volume, vhdxPath
	}
	return volume, ""
}

func (p *Provider) wslDataPath(status runtime.WSLStatus) string {
	if p.paths != nil {
		if linux, err := runtime.ToWSLPath(p.paths.DataRoot); err == nil {
			linux = strings.TrimSpace(linux)
			// /mnt/* measures a Windows volume, not the ext4 virtual disk.
			if strings.HasPrefix(linux, "/") && !strings.HasPrefix(linux, "/mnt/") {
				return linux
			}
		}
	}
	if home := strings.TrimSpace(status.Home); home != "" {
		return home
	}
	return "/"
}

func (p *Provider) wslDF(ctx context.Context, distro, linuxPath string) (uint64, uint64, uint64, error) {
	commandCtx, cancel := context.WithTimeout(ctx, wslCommandTimeout)
	defer cancel()
	result, err := p.rt.RunWSL(commandCtx, distro, "df", "-Pk", "--", linuxPath)
	if err != nil {
		detail := strings.TrimSpace(result.Stderr)
		if detail == "" {
			detail = strings.TrimSpace(result.Stdout)
		}
		if detail == "" {
			detail = err.Error()
		}
		return 0, 0, 0, fmt.Errorf("读取 WSL 磁盘使用情况失败: %s", detail)
	}
	return parseDF(result.Stdout)
}

func (p *Provider) hostVolumes(vhdxPath string) []Volume {
	type target struct {
		id    string
		path  string
		label string
	}
	targets := make([]target, 0, 2)
	seen := map[string]bool{}
	add := func(path, label string) {
		root := volumeRoot(path)
		if root == "" {
			return
		}
		drive := strings.TrimSuffix(root, `\`)
		id := "host:" + strings.ToLower(drive)
		if seen[id] {
			return
		}
		seen[id] = true
		targets = append(targets, target{id: id, path: root, label: label + "（" + drive + "）"})
	}
	if vhdxPath != "" {
		add(vhdxPath, "WSL 虚拟磁盘所在磁盘")
	}
	if p.paths != nil {
		add(p.paths.Root, "DevKit 实例所在磁盘")
	}
	if len(targets) == 0 {
		if home, err := os.UserHomeDir(); err == nil {
			add(home, "用户目录所在磁盘")
		}
	}

	volumes := make([]Volume, 0, len(targets))
	for _, item := range targets {
		volume := Volume{
			ID:        item.id,
			Kind:      "host",
			Label:     item.label,
			Path:      item.path,
			Available: false,
		}
		stat, err := hostUsage(item.path)
		if err != nil {
			volume.Error = err.Error()
			volumes = append(volumes, volume)
			continue
		}
		volume.TotalBytes = stat.total
		volume.UsedBytes = stat.used
		volume.FreeBytes = stat.free
		volume.PercentUsed = percent(stat.used, stat.total)
		volume.Available = true
		volumes = append(volumes, volume)
	}
	return volumes
}

// parseDF reads the last line of `df -Pk` output. -P guarantees one record
// per filesystem with the fields total, used, available in 1K blocks.
func parseDF(output string) (uint64, uint64, uint64, error) {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) < 2 {
		return 0, 0, 0, fmt.Errorf("无法解析 df 输出")
	}
	fields := strings.Fields(lines[len(lines)-1])
	if len(fields) < 4 {
		return 0, 0, 0, fmt.Errorf("无法解析 df 输出")
	}
	values := make([]uint64, 3)
	for i := 0; i < 3; i++ {
		kb, err := strconv.ParseUint(fields[i+1], 10, 64)
		if err != nil {
			return 0, 0, 0, fmt.Errorf("无法解析 df 输出: %w", err)
		}
		if kb > math.MaxUint64/1024 {
			return 0, 0, 0, fmt.Errorf("df 输出超出范围")
		}
		values[i] = kb * 1024
	}
	return values[0], values[1], values[2], nil
}

func percent(used, total uint64) float64 {
	if total == 0 {
		return 0
	}
	value := float64(used) / float64(total) * 100
	return math.Round(value*100) / 100
}

func volumeRoot(path string) string {
	if strings.TrimSpace(path) == "" {
		return ""
	}
	cleaned := filepath.Clean(path)
	if strings.HasPrefix(cleaned, `\\`) {
		return ""
	}
	name := filepath.VolumeName(cleaned)
	if name == "" {
		return ""
	}
	return name + `\`
}
