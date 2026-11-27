//go:build windows

package backup

import "golang.org/x/sys/windows"

func freeSpace(path string) (uint64, error) {
	pointer, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	var free, total, totalFree uint64
	if err := windows.GetDiskFreeSpaceEx(pointer, &free, &total, &totalFree); err != nil {
		return 0, err
	}
	return free, nil
}
