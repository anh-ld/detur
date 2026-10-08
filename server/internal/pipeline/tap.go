package pipeline

import (
	"html/template"
	"net/http"
	"net/url"
	"strings"

	"detur.dev/server/internal/store"
	"detur.dev/server/internal/ua"
)

// hasStoreTarget: the platform's own target is a store URL. No store target -> nothing to escape to; redirect as today.
func hasStoreTarget(link store.Link, agent string) bool {
	switch {
	case ua.IsIOS(agent):
		return isAppStoreURL(link.IOS)
	case ua.IsAndroid(agent):
		return isPlayStoreURL(link.Android)
	}
	return false
}

// intentURL: Android intent back to the short link (App Links open the app at the link), Play fallback when the app is missing. Chrome intent syntax: developer.chrome.com/docs/android/intents.
func intentURL(host, key string, q url.Values, pkg, fallback string) string {
	short := addParams("https://"+host+"/"+key, q, func(k, v string) bool { return v != "" && !internalParams[k] })
	return "intent://" + strings.TrimPrefix(short, "https://") +
		"#Intent;scheme=https;package=" + pkg + ";S.browser_fallback_url=" + url.QueryEscape(fallback) + ";end"
}

// inAppMenu: per-app "Open in browser" menu label; unlisted apps use the platform default.
var inAppMenu = map[string]string{
	"instagram": "Open in external browser",
	"threads":   "Open in external browser",
	"linkedin":  "Open in browser",
	"tiktok":    "Open in browser",
	"zalo":      "Open in browser",
	"snapchat":  "Open in browser",
	"line":      "Open in browser",
	"x":         "Open in browser",
	"telegram":  "Open in browser",
	"wechat":    "Open in browser",
}

// tapSteps: numbered "Open in browser" steps for the source app and platform.
func tapSteps(source string, ios bool) []string {
	icon := "⋮"
	if ios {
		icon = "···"
	}
	return []string{"Tap " + icon + " in the top corner.", "Choose “" + menuLabel(source, ios) + "”."}
}

// menuLabel: the app's own menu item, else a generic one for unknown webviews, else the platform browser.
func menuLabel(source string, ios bool) string {
	if l, ok := inAppMenu[source]; ok {
		return l
	}
	if source == "" || source == ua.SourceUnknownInApp {
		return "Open in browser"
	}
	if ios {
		return "Open in Safari"
	}
	return "Open in Chrome"
}

// serveTap: tap page — a plain link the user taps (in-app browsers block automatic store hand-offs). iOS copies the short link first (pasteboard signal) and reports it like the copy page.
func (p *pipelineServer) serveTap(w http.ResponseWriter, r *http.Request, link store.Link, dest, source string, q url.Values) {
	ios := ua.IsIOS(r.UserAgent())
	data := struct {
		Href  template.URL
		Play  template.URL
		IOS   bool
		Steps []string
	}{Href: template.URL(dest), IOS: ios, Steps: tapSteps(source, ios)}
	if !ios {
		// the app's package turns the button into an intent that opens the installed app at the link
		if app, err := p.st.GetApp(link.AppID); err != nil {
			p.log.Printf("tap page app lookup failed (Play button instead of intent): %v", err)
		} else if app.AndroidPackage != "" {
			data.Href = template.URL(intentURL(r.Host, link.Key, q, app.AndroidPackage, dest))
			data.Play = template.URL(dest)
		}
	}
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	_ = tapTmpl.Execute(w, data)
}

// tapTmpl: headline, one primary button (plain href: works without JS), Android-only Play text link, numbered steps. 48px full-width button, light + dark.
var tapTmpl = template.Must(template.New("tap").Parse(`<!doctype html>
<meta charset="utf-8"><meta name="robots" content="noindex"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Get the app</title>
<style>
:root{color-scheme:light dark;--bg:#fff;--fg:#111;--muted:#555;--btn:#0a66ff;--btn-fg:#fff}
@media (prefers-color-scheme:dark){:root{--bg:#111;--fg:#f2f2f2;--muted:#b5b5b5;--btn:#3d8bff}}
body{margin:0;background:var(--bg);color:var(--fg);font:17px/1.45 -apple-system,system-ui,sans-serif}
main{max-width:28rem;margin:0 auto;padding:48px 16px;display:grid;gap:20px}
h1{font-size:22px;margin:0}
.go{display:block;min-height:48px;box-sizing:border-box;padding:14px 20px;border-radius:12px;background:var(--btn);color:var(--btn-fg);font-weight:600;text-align:center;text-decoration:none}
.alt{color:var(--fg);text-align:center}
p,ol{margin:0;color:var(--muted)}
ol{padding-left:1.4em;display:grid;gap:6px}
</style>
<main>
<h1>Continue to the app</h1>
<a class="go" id="go" href="{{.Href}}">Get the app</a>
{{if .Play}}<a class="alt" href="{{.Play}}">Open Play Store</a>{{end}}
<p>Nothing happens? Open this page in your browser:</p>
<ol>{{range .Steps}}<li>{{.}}</li>{{end}}</ol>
</main>
<script>
// drop the safety-net flag from this page's URL: "Open in browser" then reopens the plain link (real browser -> store redirect)
(function () {
  var q = new URLSearchParams(location.search);
  if (!q.has("_tap")) return;
  q.delete("_tap");
  try { history.replaceState(null, "", location.pathname + "?" + q.toString()); } catch (e) {}
})();
</script>
{{if .IOS}}<script>
// copy synchronously inside the tap, report pasted_link before the navigation can unload the page
` + copyJS + `
document.getElementById("go").addEventListener("click", function () {
  var link = location.origin + location.pathname;
  if (!copy(link)) return;
  var q = new URLSearchParams(location.search);
  q.set("pasted_link", link);
  try { fetch(location.pathname + "?" + q.toString(), { keepalive: true, credentials: "omit" }).catch(function () {}); } catch (e) {}
});
</script>{{end}}`))

// copyJS: shared by the copy page and the tap page. true only when execCommand confirms the copy synchronously, so pasted_link is never reported for a copy that didn't happen. The clipboard API is a best-effort extra: its promise can't confirm before the tap navigates.
const copyJS = `function copy(text) {
  var t = document.createElement("textarea"), ok = false;
  t.value = text; t.setAttribute("readonly", ""); t.style.position = "fixed"; t.style.opacity = "0";
  document.body.appendChild(t); t.select(); t.setSelectionRange(0, text.length);
  try { ok = document.execCommand("copy"); } catch (e) {}
  document.body.removeChild(t);
  if (!ok && navigator.clipboard && navigator.clipboard.writeText) {
    try { navigator.clipboard.writeText(text).catch(function () {}); } catch (e) {}
  }
  return ok;
}`
