//go:build windows

package diskusage

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

type hostStat struct {
	total uint64
	used  uint64
	free  uint64
}

func hostUsage(path string) (hostStat, error) {
	target, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return hostStat{}, err
	}
	var freeToCaller, total, free uint64
	if err := windows.GetDiskFreeSpaceEx(target, &freeToCaller, &total, &free); err != nil {
		return hostStat{}, fmt.Errorf("读取磁盘空间 %s: %w", path, err)
	}
	if total < free {
		return hostStat{}, fmt.Errorf("磁盘 %s 返回的总空间小于可用空间", path)
	}
	return hostStat{total: total, used: total - free, free: free}, nil
}

// distroVHDX finds the ext4.vhdx file backing a WSL distribution. The file
// size is what grows on the Windows volume; deleting files inside WSL does
// not shrink it.
func distroVHDX(distro string) (string, uint64) {
	const lxssKey = `Software\Microsoft\Windows\CurrentVersion\Lxss`
	root, err := registry.OpenKey(registry.CURRENT_USER, lxssKey, registry.READ)
	if err != nil {
		return "", 0
	}
	defer root.Close()

	names, err := root.ReadSubKeyNames(-1)
	if err != nil {
		return "", 0
	}
	for _, name := range names {
		key, err := registry.OpenKey(root, name, registry.READ)
		if err != nil {
			continue
		}
		dist, _, distErr := key.GetStringValue("DistributionName")
		if distErr != nil || !strings.EqualFold(strings.TrimSpace(dist), strings.TrimSpace(distro)) {
			key.Close()
			continue
		}
		base, _, baseErr := key.GetStringValue("BasePath")
		key.Close()
		if baseErr != nil || strings.TrimSpace(base) == "" {
			continue
		}
		for _, file := range []string{"ext4.vhdx", "disk.vhdx"} {
			candidate := filepath.Join(base, file)
			info, err := os.Stat(candidate)
			if err == nil && !info.IsDir() {
				return candidate, uint64(info.Size())
			}
		}
	}
	return "", 0
}
