package api

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
)

// stubIndexHTML is the minimal landing page returned when the embedded web
// bundle is unavailable. Keeps integration tests passing before the Vue
// build step has run.
const stubIndexHTML = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>Snooze</title>
</head>
<body>
<h1>Snooze</h1>
<p>API is up. Visit <a href="/api/v1/health">/api/v1/health</a>.</p>
</body>
</html>`

// mountStatic wires the SPA's static asset routes. When rt.WebFS is set, all
// /web/* requests are served from it (with index.html as the SPA fallback);
// otherwise we serve a tiny stub so the integration suite has something to
// hit.
func (rt *Router) mountStatic(r chi.Router) {
	r.Get("/", rt.handleIndex)
	r.Get("/web", rt.redirectWeb)
	r.Get("/web/*", rt.handleWeb)
}

// handleIndex serves the SPA root or the stub HTML.
func (rt *Router) handleIndex(w http.ResponseWriter, r *http.Request) {
	if rt.WebFS != nil {
		rt.serveFile(w, r, "/index.html")
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(stubIndexHTML))
}

// redirectWeb canonicalises /web → /web/.
func (rt *Router) redirectWeb(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/web/", http.StatusMovedPermanently)
}

// handleWeb serves files under /web/ from rt.WebFS, falling back to the SPA
// index.html for unmatched paths (so the Vue router can take over).
func (rt *Router) handleWeb(w http.ResponseWriter, r *http.Request) {
	if rt.WebFS == nil {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(stubIndexHTML))
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/web")
	if path == "" || path == "/" {
		path = "/index.html"
	}
	rt.serveFile(w, r, path)
}

// serveFile streams a single file from WebFS, falling back to /index.html
// on miss (the canonical SPA-fallback behaviour).
func (rt *Router) serveFile(w http.ResponseWriter, r *http.Request, path string) {
	f, err := rt.WebFS.Open(path)
	if err != nil {
		if path != "/index.html" {
			rt.serveFile(w, r, "/index.html")
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(stubIndexHTML))
		return
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		WriteError(w, r, ErrInternal.WithCause(err))
		return
	}
	if info.IsDir() {
		rt.serveFile(w, r, strings.TrimSuffix(path, "/")+"/index.html")
		return
	}
	http.ServeContent(w, r, info.Name(), info.ModTime(), readSeeker(f))
}

// readSeeker is a tiny adapter so http.ServeContent gets a *bytes.Reader when
// the underlying file does not directly implement io.ReadSeeker. We use the
// type-asserting path first to avoid copying when the FS supports seeking
// natively (which the http.Dir/embed.FS do).
type seekableFile interface {
	Read([]byte) (int, error)
	Seek(int64, int) (int64, error)
}

func readSeeker(f http.File) seekableFile {
	// http.File is io.ReadSeeker by interface, so this is a free assertion.
	return f
}
