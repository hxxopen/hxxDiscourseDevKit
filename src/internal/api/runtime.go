package api

import (
	"net/http"

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
