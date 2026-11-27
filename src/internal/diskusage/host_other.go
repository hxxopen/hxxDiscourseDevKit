//go:build !windows

package diskusage

import "errors"

type hostStat struct {
	total uint64
	used  uint64
	free  uint64
}

func hostUsage(string) (hostStat, error) {
	return hostStat{}, errors.New("当前平台不支持 Windows 磁盘统计")
}

func distroVHDX(string) (string, uint64) {
	return "", 0
}
