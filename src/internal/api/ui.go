package api

import (
	"io/fs"
	"net/http"
	"path"
	"strings"

	"github.com/hxx/hxxDiscourseDevKit/internal/httpx"
)

const fallbackUI = `<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>hxxDiscourseDevKit</title>
  <style>
    body { font-family: system-ui, -apple-system, "Segoe UI", sans-serif; background:#0f172a; color:#e2e8f0;
           display:flex; min-height:100vh; align-items:center; justify-content:center; margin:0; }
    main { max-width:640px; padding:40px; background:#1e293b; border-radius:16px; border:1px solid #334155; }
    h1 { margin:0 0 12px; font-size:22px; }
    p { line-height:1.7; color:#cbd5e1; }
    code { background:#0f172a; padding:2px 6px; border-radius:6px; color:#7dd3fc; }
  </style>
</head>
<body>
<main>
  <h1>管理 UI 尚未构建</h1>
  <p>devkitd 已经运行，但当前二进制中没有前端构建产物。</p>
  <p>开发模式请启动 <code>npm --prefix src/web run dev</code>，然后访问 <code>http://127.0.0.1:5173</code>。</p>
  <p>生产构建请运行 <code>npm --prefix src/web run build</code>，再重新编译 devkitd。</p>
  <p>API 健康检查：<code>/api/v1/system/health</code></p>
</main>
</body>
</html>`

func (s *Server) handleUI(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") {
		httpx.WriteError(w, r, httpx.NotFound("NOT_FOUND", "API 路径不存在"), s.log)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		httpx.WriteError(w, r,
			httpx.New(http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "method not allowed"), s.log)
		return
	}

	name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
	if name == "" || name == "." {
		name = "index.html"
	}

	if s.opts.UI != nil {
		if file, err := s.opts.UI.Open(name); err == nil {
			info, statErr := file.Stat()
			_ = file.Close()
			if statErr == nil && !info.IsDir() {
				if strings.HasPrefix(name, "assets/") {
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				} else {
					w.Header().Set("Cache-Control", "no-store")
				}
				http.ServeFileFS(w, r, s.opts.UI, name)
				return
			}
		}
		if index, err := fs.ReadFile(s.opts.UI, "index.html"); err == nil {
			if isStaticAssetPath(name) {
				httpx.WriteError(w, r, httpx.NotFound("NOT_FOUND", "静态资源不存在"), s.log)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Cache-Control", "no-store")
			w.WriteHeader(http.StatusOK)
			if r.Method != http.MethodHead {
				_, _ = w.Write(index)
			}
			return
		}
	}

	if isStaticAssetPath(name) {
		httpx.WriteError(w, r, httpx.NotFound("NOT_FOUND", "静态资源不存在"), s.log)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write([]byte(fallbackUI))
	}
}

// isStaticAssetPath reports whether a request path names a file rather than a
// client-side route. Hash-based routing needs no extensionless fallback, so
// missing assets should be a real 404 instead of index.html with a JS MIME
// type mismatch.
func isStaticAssetPath(name string) bool {
	return strings.HasPrefix(name, "assets/") || path.Ext(name) != ""
}
