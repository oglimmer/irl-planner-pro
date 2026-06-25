package server

import (
	"html/template"
	"net/http"
	"strings"
)

// errorPageTmpl renders a friendly standalone HTML page for a browser that
// lands on an unmatched backend route directly. The SPA (served by nginx) owns
// the in-app error experience; this is the fallback for a human who hits the
// API host with a browser.
var errorPageTmpl = template.Must(template.New("error").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>{{.Code}} — {{.Title}}</title>
<style>
*{box-sizing:border-box}
body{font-family:ui-sans-serif,system-ui,-apple-system,sans-serif;display:flex;align-items:center;justify-content:center;min-height:100vh;margin:0;background:#f7f7f9;color:#1d2330}
.card{background:#fff;border:1px solid #e6e8ef;padding:2.75rem 3rem;max-width:480px;border-radius:12px;box-shadow:0 1px 3px rgba(20,30,60,.06)}
.code{font-size:.7rem;letter-spacing:.22em;text-transform:uppercase;color:#6b72ff;margin:0 0 .85rem}
h1{font-weight:650;font-size:1.6rem;margin:0 0 1rem;line-height:1.15}
p.lede{color:#5b6478;font-size:.95rem;line-height:1.55;margin:0 0 1.5rem}
a{color:#3b49ff;text-decoration:none;font-size:.85rem}
a:hover{text-decoration:underline}
</style>
</head>
<body>
<div class="card">
<p class="code">Error {{.Code}}</p>
<h1>{{.Title}}</h1>
<p class="lede">{{.Message}}</p>
<a href="/">← Back to the app</a>
</div>
</body>
</html>`))

type errorPageData struct {
	Code    int
	Title   string
	Message string
}

// writeHTTPError responds with a content-negotiated error: a browser
// (Accept: text/html) gets the styled HTML page; every other client gets the
// standard {"error":"..."} JSON used throughout the API.
func writeHTTPError(w http.ResponseWriter, r *http.Request, status int, jsonMsg, title, detail string) {
	if acceptsHTML(r) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(status)
		errorPageTmpl.Execute(w, errorPageData{Code: status, Title: title, Message: detail})
		return
	}
	writeErr(w, status, jsonMsg)
}

func acceptsHTML(r *http.Request) bool {
	return strings.Contains(r.Header.Get("Accept"), "text/html")
}

func (a *App) handleNotFound(w http.ResponseWriter, r *http.Request) {
	writeHTTPError(w, r, http.StatusNotFound, "not found",
		"Page not found",
		"The page or resource you requested doesn't exist. It may have been moved or removed.")
}

func (a *App) handleMethodNotAllowed(w http.ResponseWriter, r *http.Request) {
	writeHTTPError(w, r, http.StatusMethodNotAllowed, "method not allowed",
		"Method not allowed",
		"That action isn't supported for this URL.")
}
