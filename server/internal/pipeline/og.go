package pipeline

import (
	"html/template"
	"net/http"
	"net/url"

	"detur.dev/server/internal/httpx"
	"detur.dev/server/internal/store"
)

// serveOG: social preview for link-preview crawlers. Humans misread as bots (C3 false positive, stray ?bot=) still go on: script redirect + Continue link to the 302 target. Crawlers run no script.
func serveOG(w http.ResponseWriter, r *http.Request, link store.Link, dest string, q url.Values) {
	card := "summary"
	if link.OGImage != "" {
		card = "summary_large_image"
	}
	if httpx.IsRoute(dest) { // app route: nothing a browser can follow -> preview only
		dest = ""
	}
	htmlNoStore(w)
	_ = ogTmpl.Execute(w, struct {
		Title, Desc, Card, Short string
		Image, Href              template.URL // OGImage https-checked at save; dest = saved target (R6 rejects script schemes)
		Dest                     string
	}{link.OGTitle, link.OGDescription, card, shortURL(r, link.Key, q), template.URL(link.OGImage), template.URL(dest), dest})
}

var ogTmpl = template.Must(template.New("og").Parse(`<!doctype html>
<meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>{{.Title}}</title>
<link rel="canonical" href="{{.Short}}">
<meta property="og:url" content="{{.Short}}">
<meta property="og:type" content="website">
{{if .Title}}<meta property="og:title" content="{{.Title}}"><meta name="twitter:title" content="{{.Title}}">{{end}}
{{if .Desc}}<meta name="description" content="{{.Desc}}"><meta property="og:description" content="{{.Desc}}"><meta name="twitter:description" content="{{.Desc}}">{{end}}
{{if .Image}}<meta property="og:image" content="{{.Image}}"><meta name="twitter:image" content="{{.Image}}">{{end}}
<meta name="twitter:card" content="{{.Card}}">
{{if .Href}}<p><a href="{{.Href}}">Continue</a></p>
<script>location.replace({{.Dest}});</script>{{end}}`))
