package api

// staticHandler serves the built frontend (web/dist) under the same
// global secure() boundary as the REST API — Basic Auth everywhere except
// /api/health, and nothing under /api/ ever reaches this handler (unknown
// API paths get a JSON 404, never the SPA shell). Client routes without a
// file on disk (/, /history, /settings, …) fall back to index.html so the
// React router can take over. Paths that resolve to an existing file are
// served verbatim (assets, favicons).
//
// http.FileServer/http.Dir already refuse "." and ".." escapes; we also
// check existence *inside* the web dir before deciding to fall back, so a
// probe cannot learn about files outside the web root from the status
// difference — a traversal attempt yields the same index.html shell.

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

func staticHandler(webDir string) http.Handler {
	fileServer := http.FileServer(http.Dir(webDir))
	index := filepath.Join(webDir, "index.html")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			writeErr(w, http.StatusNotFound, "not_found", "resource not found")
			return
		}
		if r.URL.Path != "/" && !hasFile(webDir, r.URL.Path) {
			// SPA fallback: the client owns the route
			http.ServeFile(w, r, index)
			return
		}
		fileServer.ServeHTTP(w, r)
	})
}

// hasFile reports whether URLPath maps to a real file (not a directory)
// inside webDir. http.Dir's escaping rules apply: anything outside the
// root is treated as missing.
func hasFile(webDir, urlPath string) bool {
	p := filepath.Join(webDir, filepath.FromSlash(strings.TrimPrefix(urlPath, "/")))
	rel, err := filepath.Rel(webDir, p)
	if err != nil || strings.HasPrefix(rel, "..") {
		return false
	}
	fi, err := filepath.EvalSymlinks(p)
	if err != nil {
		return false
	}
	info, err := os.Stat(fi)
	return err == nil && !info.IsDir()
}
