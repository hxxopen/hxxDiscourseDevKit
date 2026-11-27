package api_test

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hxx/hxxDiscourseDevKit/internal/audit"
)

func auditList(t *testing.T, s *stack, query string) audit.ListResult {
	t.Helper()
	resp, body := s.do(http.MethodGet, "/api/v1/audit"+query, nil, s.tokenAuth)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /audit%s status = %d, body = %s", query, resp.StatusCode, body)
	}
	var page audit.ListResult
	decodeJSON(t, body, &page)
	return page
}

func TestAuditRecordsMutationAndRejection(t *testing.T) {
	s := newStack(t)

	resp, body := s.do(http.MethodPost, "/api/v1/workspaces",
		map[string]string{"name": "Audit Alpha"}, func(req *http.Request) {
			s.tokenAuth(req)
			req.Header.Set("X-DevKit-Client", "cli")
		})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", resp.StatusCode, body)
	}

	page := auditList(t, s, "?action=workspace.create")
	if page.Total != 1 || len(page.Items) != 1 {
		t.Fatalf("create audit = %+v", page)
	}
	created := page.Items[0]
	if created.Result != "success" || created.Status != http.StatusCreated {
		t.Fatalf("create event result/status = %+v", created)
	}
	if created.Client != "cli" || created.Actor != "local" {
		t.Fatalf("create event client/actor = %+v", created)
	}
	if created.Method != http.MethodPost || created.Path != "/api/v1/workspaces" {
		t.Fatalf("create event request fields = %+v", created)
	}
	if created.RequestID == "" || created.ID == "" || created.DurationMS < 0 {
		t.Fatalf("create event metadata = %+v", created)
	}
	if created.WorkspaceID != "" {
		t.Fatalf("create event workspace ID = %q, want empty", created.WorkspaceID)
	}

	resp, body = s.do(http.MethodPost, "/api/v1/workspaces",
		map[string]string{"name": "Unauthorized"}, nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated create status = %d, body = %s", resp.StatusCode, body)
	}

	page = auditList(t, s, "?action=workspace.create&result=failure")
	if page.Total != 1 || len(page.Items) != 1 {
		t.Fatalf("rejection audit = %+v", page)
	}
	rejected := page.Items[0]
	if rejected.Status != http.StatusUnauthorized || rejected.Result != "failure" {
		t.Fatalf("rejection event = %+v", rejected)
	}

	page = auditList(t, s, "?limit=500")
	for _, event := range page.Items {
		if event.Method == http.MethodGet {
			t.Fatalf("GET request was audited: %+v", event)
		}
	}

	resp, body = s.do(http.MethodGet, "/api/v1/audit", nil, nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated audit status = %d, body = %s", resp.StatusCode, body)
	}

	resp, body = s.do(http.MethodGet, "/api/v1/system/info", nil, s.tokenAuth)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("info status = %d, body = %s", resp.StatusCode, body)
	}
	var info struct {
		Capabilities map[string]bool `json:"capabilities"`
	}
	decodeJSON(t, body, &info)
	if !info.Capabilities["audit"] {
		t.Fatal("audit capability is not advertised")
	}
}

func TestAuditNeverStoresRequestBody(t *testing.T) {
	s := newStack(t)
	const key = "audit-secret-key-0123456789"

	resp, body := s.do(http.MethodPut, "/api/v1/settings/theme-preview",
		map[string]string{"api_key": key}, func(req *http.Request) {
			s.tokenAuth(req)
			req.Header.Set("X-DevKit-Client", "ui")
		})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("save key status = %d, body = %s", resp.StatusCode, body)
	}

	page := auditList(t, s, "?action=settings.theme-preview.set")
	if page.Total != 1 || len(page.Items) != 1 {
		t.Fatalf("key audit = %+v", page)
	}
	if page.Items[0].Client != "ui" || page.Items[0].Result != "success" {
		t.Fatalf("key event = %+v", page.Items[0])
	}

	entries, err := os.ReadDir(s.auditDir)
	if err != nil {
		t.Fatalf("read audit dir: %v", err)
	}
	var raw strings.Builder
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.auditDir, entry.Name()))
		if err != nil {
			t.Fatalf("read audit file: %v", err)
		}
		raw.Write(data)
	}
	if strings.Contains(raw.String(), key) {
		t.Fatal("audit trail contains the API key from the request body")
	}
	if strings.Contains(raw.String(), "api_key") {
		t.Fatal("audit trail contains a request body field name")
	}
}

func TestAuditCapturesWorkspaceAndTarget(t *testing.T) {
	s := newStack(t)
	resp, body := s.do(http.MethodPost, "/api/v1/workspaces",
		map[string]string{"name": "Audit Target"}, s.tokenAuth)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", resp.StatusCode, body)
	}
	var created struct {
		WorkspaceID string `json:"workspace_id"`
	}
	decodeJSON(t, body, &created)

	resp, body = s.do(http.MethodPost,
		"/api/v1/workspaces/"+created.WorkspaceID+"/plugins/demo/test", nil, s.tokenAuth)
	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusCreated {
		t.Fatalf("plugin test unexpectedly succeeded: %d %s", resp.StatusCode, body)
	}

	page := auditList(t, s, "?action=plugin.test&workspace_id="+created.WorkspaceID)
	if page.Total != 1 || len(page.Items) != 1 {
		t.Fatalf("plugin test audit = %+v", page)
	}
	event := page.Items[0]
	if event.WorkspaceID != created.WorkspaceID {
		t.Fatalf("workspace ID = %q, want %q", event.WorkspaceID, created.WorkspaceID)
	}
	if event.Target != "demo" {
		t.Fatalf("target = %q, want demo", event.Target)
	}
	if event.Result != "failure" {
		t.Fatalf("result = %q, want failure", event.Result)
	}
}

func TestAuditQueryValidation(t *testing.T) {
	s := newStack(t)
	for _, query := range []string{
		"?limit=0",
		"?limit=501",
		"?result=unknown",
		"?since=not-a-time",
	} {
		resp, body := s.do(http.MethodGet, "/api/v1/audit"+query, nil, s.tokenAuth)
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("GET /audit%s status = %d, body = %s", query, resp.StatusCode, body)
		}
	}
}
