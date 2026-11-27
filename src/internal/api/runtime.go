package api

import (
	"context"
	"net/http"
	"time"

	"github.com/hxx/hxxDiscourseDevKit/internal/httpx"
	"github.com/hxx/hxxDiscourseDevKit/internal/runtime"
)

type runtimeMatrixResponse struct {
	Matrix  runtime.Matrix `json:"matrix"`
	Builtin bool           `json:"builtin"`
}

func (s *Server) handleRuntimeMatrix(w http.ResponseWriter, r *http.Request) {
	if s.opts.Runtime == nil {
		s.fail(w, r, errFeatureUnavailable("runtime"))
		return
	}
	matrix, builtin, err := s.opts.Runtime.Matrix()
	if err != nil {
		s.fail(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, runtimeMatrixResponse{Matrix: matrix, Builtin: builtin})
}

type runtimeComponentsResponse struct {
	Items   []runtime.ComponentStatus `json:"items"`
	Total   int                       `json:"total"`
	Profile string                    `json:"profile"`
	Builtin bool                      `json:"builtin"`
}

func (s *Server) handleListRuntimeComponents(w http.ResponseWriter, r *http.Request) {
	if s.opts.Runtime == nil {
		s.fail(w, r, errFeatureUnavailable("runtime"))
		return
	}
	matrix, builtin, err := s.opts.Runtime.Matrix()
	if err != nil {
		s.fail(w, r, err)
		return
	}
	items, err := s.opts.Runtime.List(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, runtimeComponentsResponse{
		Items:   items,
		Total:   len(items),
		Profile: matrix.Profile,
		Builtin: builtin,
	})
}

func (s *Server) handleGetRuntimeComponent(w http.ResponseWriter, r *http.Request) {
	if s.opts.Runtime == nil {
		s.fail(w, r, errFeatureUnavailable("runtime"))
		return
	}
	item, err := s.opts.Runtime.Verify(r.Context(), r.PathValue("name"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, item)
}

func (s *Server) handleVerifyRuntime(w http.ResponseWriter, r *http.Request) {
	if s.opts.Runtime == nil {
		s.fail(w, r, errFeatureUnavailable("runtime"))
		return
	}
	items, err := s.opts.Runtime.List(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, runtimeComponentsResponse{Items: items, Total: len(items)})
}

func (s *Server) handleRuntimeWSL(w http.ResponseWriter, r *http.Request) {
	if s.opts.Runtime == nil {
		s.fail(w, r, errFeatureUnavailable("runtime"))
		return
	}
	httpx.WriteJSON(w, http.StatusOK, s.opts.Runtime.WSLStatus(r.Context()))
}

type runtimeInstallPreviewInput struct {
	Components []string `json:"components"`
}

func (s *Server) handlePreviewRuntimeInstall(w http.ResponseWriter, r *http.Request) {
	if s.opts.Runtime == nil {
		s.fail(w, r, errFeatureUnavailable("runtime"))
		return
	}
	var input runtimeInstallPreviewInput
	if err := decodeOptionalBody(w, r, &input); err != nil {
		httpx.WriteError(w, r, err, s.log)
		return
	}
	plan, err := s.opts.Runtime.PreviewInstall(r.Context(), input.Components)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, plan)
}

type runtimeInstallInput struct {
	ConfirmToken string `json:"confirm_token"`
}

func (s *Server) handleInstallRuntime(w http.ResponseWriter, r *http.Request) {
	if s.opts.Runtime == nil {
		s.fail(w, r, errFeatureUnavailable("runtime"))
		return
	}
	if s.opts.Tasks == nil {
		s.fail(w, r, errFeatureUnavailable("任务"))
		return
	}
	var input runtimeInstallInput
	if err := httpx.DecodeJSON(w, r, &input); err != nil {
		httpx.WriteError(w, r, err, s.log)
		return
	}
	if input.ConfirmToken == "" {
		s.fail(w, r, httpx.Unprocessable("PREVIEW_REQUIRED", "安装操作需要有效的预览确认令牌"))
		return
	}
	submitted := s.opts.Tasks.Submit("runtime.install", "", true,
		func(ctx context.Context, progress func(int, string)) error {
			return s.opts.Runtime.Install(ctx, input.ConfirmToken, progress)
		})
	httpx.WriteJSON(w, http.StatusAccepted, deleteAcceptedResponse{
		TaskID:    submitted.TaskID,
		StatusURL: "/api/v1/tasks/" + submitted.TaskID,
		Task:      submitted,
	})
}

type runtimeDiscoverResponse struct {
	Components map[string]runtime.DiscoveredComponent `json:"components"`
	Names      []string                               `json:"names"`
	Total      int                                    `json:"total"`
	UpdatedAt  time.Time                              `json:"updated_at"`
}

func (s *Server) handleDiscoverRuntime(w http.ResponseWriter, r *http.Request) {
	if s.opts.Runtime == nil {
		s.fail(w, r, errFeatureUnavailable("runtime"))
		return
	}
	result, err := s.opts.Runtime.Discover(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	names := result.Names()
	httpx.WriteJSON(w, http.StatusOK, runtimeDiscoverResponse{
		Components: result.Components,
		Names:      names,
		Total:      len(names),
		UpdatedAt:  result.UpdatedAt,
	})
}
