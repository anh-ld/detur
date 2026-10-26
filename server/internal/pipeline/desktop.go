package pipeline

import (
	"encoding/base64"
	"html/template"
	"net/http"
	"net/url"
	"strings"

	"rsc.io/qr"

	"detur.dev/server/internal/httpx"
	"detur.dev/server/internal/store"
)

// isWebURL: http(s) target a desktop browser can open.
func isWebURL(s string) bool {
	u, err := url.Parse(s)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https")
}

// shortURL: the visited short link, forwarded params kept, internal ones dropped.
func shortURL(r *http.Request, key string, q url.Values) string {
	scheme := "https"
	if r.TLS == nil && strings.Contains(r.Host, ":") { // dev listener (host:port, plain HTTP); prod sits behind TLS on the bare domain
		scheme = "http"
	}
	return httpx.AddParams(scheme+"://"+r.Host+"/"+key, q, func(k, v string) bool { return v != "" && !internalParams[k] })
}

// serveDesktop: desktop visitor whose target is an app scheme (myapp://): a 302 there opens nothing. Page = QR + short link to open on a phone, store buttons when the link has them.
func (p *pipelineServer) serveDesktop(w http.ResponseWriter, r *http.Request, link store.Link, q url.Values) {
	short := shortURL(r, link.Key, q)
	data := struct {
		Name, Short string
		QR          template.URL
		IOS, Play   template.URL
	}{Name: "the app", Short: short}
	if app, err := p.st.GetApp(link.AppID); err == nil && app.Name != "" {
		data.Name = app.Name
	}
	if code, err := qr.Encode(short, qr.M); err == nil {
		code.Scale = 6
		data.QR = template.URL("data:image/png;base64," + base64.StdEncoding.EncodeToString(code.PNG()))
	}
	if isAppStoreURL(link.IOS) && isWebURL(link.IOS) {
		data.IOS = template.URL(link.IOS)
	}
	if isPlayStoreURL(link.Android) && isWebURL(link.Android) {
		data.Play = template.URL(link.Android)
	}
	htmlNoStore(w)
	_ = desktopTmpl.Execute(w, data)
}

// desktopTmpl: same look as the tap page. QR + selectable link are the minimum; store buttons only when set.
var desktopTmpl = template.Must(template.New("desktop").Parse(`<!doctype html>
<meta charset="utf-8"><meta name="robots" content="noindex"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Open {{.Name}} on your phone</title>
<style>
:root{color-scheme:light dark;--bg:#fff;--fg:#111;--muted:#555;--btn:#0a66ff;--btn-fg:#fff;--line:#ddd}
@media (prefers-color-scheme:dark){:root{--bg:#111;--fg:#f2f2f2;--muted:#b5b5b5;--btn:#3d8bff;--line:#333}}
body{margin:0;background:var(--bg);color:var(--fg);font:17px/1.45 -apple-system,system-ui,sans-serif}
main{max-width:28rem;margin:0 auto;padding:48px 16px;display:grid;gap:20px;text-align:center}
h1{font-size:22px;margin:0}
p{margin:0;color:var(--muted)}
img{justify-self:center;background:#fff;padding:8px;border-radius:12px;image-rendering:pixelated}
.link{display:flex;gap:8px;border:1px solid var(--line);border-radius:12px;padding:8px 8px 8px 14px;align-items:center;text-align:left}
.link code{flex:1;overflow-wrap:anywhere;user-select:all}
button,.go{font:inherit;font-weight:600;border:0;border-radius:10px;padding:10px 16px;background:var(--btn);color:var(--btn-fg);text-decoration:none;cursor:pointer}
.stores{display:flex;gap:12px;justify-content:center;flex-wrap:wrap}
</style>
<main>
<h1>Open {{.Name}} on your phone</h1>
<p>Scan the code with your phone camera, or send yourself the link.</p>
{{if .QR}}<img src="{{.QR}}" width="240" height="240" alt="QR code for {{.Short}}">{{end}}
<div class="link"><code id="short">{{.Short}}</code><button id="copy" type="button">Copy</button></div>
{{if or .IOS .Play}}<div class="stores">{{if .IOS}}<a class="go" href="{{.IOS}}">App Store</a>{{end}}{{if .Play}}<a class="go" href="{{.Play}}">Google Play</a>{{end}}</div>{{end}}
</main>
<script>
` + copyJS + `
document.getElementById("copy").addEventListener("click", function () {
  if (copy(document.getElementById("short").textContent)) this.textContent = "Copied";
});
</script>`))
