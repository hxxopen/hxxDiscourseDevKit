package migration

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/hxx/hxxDiscourseDevKit/internal/runtime"
)

type wslCopier struct {
	runtime *runtime.Manager
	distro  string
}

// NewWSLCopier copies files inside a WSL distribution. It is used instead of
// Windows-side file APIs so Unix modes and symlinks are preserved.
func NewWSLCopier(manager *runtime.Manager, distro string) Copier {
	return &wslCopier{runtime: manager, distro: strings.TrimSpace(distro)}
}

func (c *wslCopier) EnsureDir(ctx context.Context, hostPath string, mode os.FileMode) error {
	return c.runtime.EnsureDir(ctx, runtime.WSLExecution(c.distro), hostPath, mode)
}

func (c *wslCopier) CopyTree(ctx context.Context, srcHost, dstHost string) error {
	src, err := runtime.ToWSLPath(srcHost)
	if err != nil {
		return err
	}
	dst, err := runtime.ToWSLPath(dstHost)
	if err != nil {
		return err
	}
	result, err := c.runtime.RunWSL(ctx, c.distro, "cp", "-a", "--", src, dst)
	if err != nil {
		return fmt.Errorf("复制 %s 失败: %s", src, wslError(result, err))
	}
	return nil
}

func (c *wslCopier) Move(ctx context.Context, srcHost, dstHost string) error {
	src, err := runtime.ToWSLPath(srcHost)
	if err != nil {
		return err
	}
	dst, err := runtime.ToWSLPath(dstHost)
	if err != nil {
		return err
	}
	// -T refuses to move into an existing directory, which would otherwise
	// create dst/<basename> instead of replacing a missing destination.
	result, err := c.runtime.RunWSL(ctx, c.distro, "mv", "-T", "--", src, dst)
	if err != nil {
		return fmt.Errorf("移动 %s 失败: %s", src, wslError(result, err))
	}
	return nil
}

func (c *wslCopier) RemoveAll(ctx context.Context, hostPath string) error {
	target, err := runtime.ToWSLPath(hostPath)
	if err != nil {
		return err
	}
	result, err := c.runtime.RunWSL(ctx, c.distro, "rm", "-rf", "--", target)
	if err != nil {
		return fmt.Errorf("删除 %s 失败: %s", target, wslError(result, err))
	}
	return nil
}

func (c *wslCopier) FreeBytes(ctx context.Context, hostPath string) (int64, error) {
	target, err := runtime.ToWSLPath(hostPath)
	if err != nil {
		return 0, err
	}
	result, err := c.runtime.RunWSL(ctx, c.distro, "df", "-Pk", "--", target)
	if err != nil {
		return 0, fmt.Errorf("检查可用空间失败: %s", wslError(result, err))
	}
	lines := strings.Split(strings.TrimSpace(result.Stdout), "\n")
	if len(lines) < 2 {
		return 0, fmt.Errorf("无法解析 df 输出")
	}
	fields := strings.Fields(lines[len(lines)-1])
	if len(fields) < 4 {
		return 0, fmt.Errorf("无法解析 df 输出")
	}
	kb, err := strconv.ParseInt(fields[3], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("无法解析 df 可用空间: %w", err)
	}
	return kb * 1024, nil
}

func wslError(result runtime.RunResult, err error) string {
	message := strings.TrimSpace(result.Stderr)
	if message == "" {
		message = strings.TrimSpace(result.Stdout)
	}
	if message == "" {
		message = err.Error()
	}
	return message
}
