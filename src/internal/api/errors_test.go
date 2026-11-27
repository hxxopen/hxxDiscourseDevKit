package api

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/hxx/hxxDiscourseDevKit/internal/httpx"
	"github.com/hxx/hxxDiscourseDevKit/internal/service"
)

func TestMapErrorSurfacesServiceDataDirProblem(t *testing.T) {
	cause := fmt.Errorf("%w: 数据目录位于 /mnt/d，请迁移到 WSL2 文件系统", service.ErrDataDirUnsupported)
	err := mapError(cause)

	var apiErr *httpx.Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("mapError returned %T, want *httpx.Error", err)
	}
	if apiErr.Status != http.StatusConflict {
		t.Errorf("status = %d, want %d", apiErr.Status, http.StatusConflict)
	}
	if apiErr.Code != "SERVICE_DATA_DIR_UNSUPPORTED" {
		t.Errorf("code = %q", apiErr.Code)
	}
	if !strings.Contains(apiErr.Message, "WSL2") {
		t.Errorf("message lost the actionable detail: %q", apiErr.Message)
	}
}
