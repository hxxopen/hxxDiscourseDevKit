package api_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/hxx/hxxDiscourseDevKit/internal/api"
	"github.com/hxx/hxxDiscourseDevKit/internal/diskusage"
)

type fakeDiskUsage struct {
	report diskusage.Report
}

func (f fakeDiskUsage) Report(context.Context) diskusage.Report {
	return f.report
}

func TestDiskUsageEndpoint(t *testing.T) {
	report := diskusage.Report{
		GeneratedAt: time.Date(2026, 11, 27, 10, 0, 0, 0, time.UTC),
		Volumes: []diskusage.Volume{
			{
				ID:          "wsl",
				Kind:        "wsl",
				Label:       "WSL 虚拟磁盘（Ubuntu-24.04）",
				Path:        "/home/fushou/hxxwork",
				TotalBytes:  1081102131200,
				UsedBytes:   3004703772,
				FreeBytes:   1023119027200,
				PercentUsed: 0.28,
				VHDXPath:    `F:\vmfiles\wsl2ubuntu2404\ext4.vhdx`,
				VHDXBytes:   3929014272,
				Available:   true,
			},
		},
	}
	s := newStack(t, func(opts *api.Options) {
		opts.DiskUsage = fakeDiskUsage{report: report}
	})

	resp, body := s.do(http.MethodGet, "/api/v1/system/disk", nil, s.tokenAuth)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body = %s", resp.StatusCode, body)
	}
	var got diskusage.Report
	decodeJSON(t, body, &got)
	if !got.GeneratedAt.Equal(report.GeneratedAt) {
		t.Fatalf("generated_at = %s, want %s", got.GeneratedAt, report.GeneratedAt)
	}
	if len(got.Volumes) != 1 {
		t.Fatalf("volumes = %d, want 1", len(got.Volumes))
	}
	volume := got.Volumes[0]
	if volume.ID != "wsl" || volume.VHDXBytes != report.Volumes[0].VHDXBytes {
		t.Fatalf("volume = %+v", volume)
	}

	resp, body = s.do(http.MethodGet, "/api/v1/system/info", nil, s.tokenAuth)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("info status = %d, body = %s", resp.StatusCode, body)
	}
	var info struct {
		Capabilities map[string]bool `json:"capabilities"`
	}
	decodeJSON(t, body, &info)
	if !info.Capabilities["disk_usage"] {
		t.Fatalf("disk_usage capability = false, want true")
	}
}

func TestDiskUsageUnavailable(t *testing.T) {
	s := newStack(t)

	resp, body := s.do(http.MethodGet, "/api/v1/system/disk", nil, s.tokenAuth)
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, body = %s", resp.StatusCode, body)
	}
	if code := errorCode(t, body); code != "FEATURE_UNAVAILABLE" {
		t.Fatalf("error code = %q", code)
	}

	resp, body = s.do(http.MethodGet, "/api/v1/system/info", nil, s.tokenAuth)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("info status = %d, body = %s", resp.StatusCode, body)
	}
	var info struct {
		Capabilities map[string]bool `json:"capabilities"`
	}
	decodeJSON(t, body, &info)
	if info.Capabilities["disk_usage"] {
		t.Fatalf("disk_usage capability = true, want false")
	}
}
