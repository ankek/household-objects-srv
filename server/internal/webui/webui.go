package webui

import (
	"embed"
	"errors"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

//go:embed all:dist
var dist embed.FS

const indexFile = "index.html"

const assetsPrefix = "assets/"

func Handler() http.Handler {
	root, err := fs.Sub(dist, "dist")
	if err != nil {
		return notBuiltHandler()
	}
	if _, err := fs.Stat(root, indexFile); err != nil {
		return notBuiltHandler()
	}

	files := http.FileServerFS(root)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if name == "" {
			name = indexFile
		}

		if _, err := fs.Stat(root, name); err != nil {
			if !errors.Is(err, fs.ErrNotExist) {
				http.Error(w, "internal server error", http.StatusInternalServerError)
				return
			}
			if strings.HasPrefix(name, assetsPrefix) {
				http.NotFound(w, r)
				return
			}
			serveIndex(w, r, root)
			return
		}

		setCacheHeaders(w, name)
		files.ServeHTTP(w, r)
	})
}

func serveIndex(w http.ResponseWriter, r *http.Request, root fs.FS) {
	body, err := fs.ReadFile(root, indexFile)
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	setCacheHeaders(w, indexFile)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(body)
}

func setCacheHeaders(w http.ResponseWriter, name string) {
	if strings.HasPrefix(name, assetsPrefix) {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		return
	}
	w.Header().Set("Cache-Control", "no-cache")
}

const notBuiltPage = `<!doctype html>
<html lang="en"><head><meta charset="utf-8"><title>HHO — web UI not built</title>
<style>body{font:16px/1.6 system-ui,sans-serif;margin:4rem auto;max-width:40rem;padding:0 1rem}
code{background:#f4f4f5;padding:.15em .4em;border-radius:4px}</style></head>
<body>
<h1>Web UI not built</h1>
<p>This binary was compiled without the Vue SPA, so there is nothing to serve here. The API itself
is unaffected and is answering on <code>/api/v1</code>.</p>
<p>To build it:</p>
<pre><code>cd server/web
npm ci
npm run build
go -C .. build ./cmd/hho</code></pre>
<p>The published container image always ships the built UI; seeing this page means a local build
skipped the frontend step.</p>
</body></html>
`

func notBuiltHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusOK)
		if r.Method == http.MethodHead {
			return
		}
		_, _ = w.Write([]byte(notBuiltPage))
	})
}
