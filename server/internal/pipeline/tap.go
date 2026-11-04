package pipeline

import (
	"html/template"
	"net/http"
	"net/url"
	"strings"

	"detur.dev/server/internal/httpx"
	"detur.dev/server/internal/store"
	"detur.dev/server/internal/ua"
)

// hasStoreTarget: platform's own target is a store URL. None -> nothing to escape to; plain redirect.
func hasStoreTarget(link store.Link, agent string) bool {
	switch {
	case ua.IsIOS(agent):
		return isAppStoreURL(link.IOS)
	case ua.IsAndroid(agent):
		return isPlayStoreURL(link.Android)
	}
	return false
}

// intentURL: Android intent back to short link (App Links open app at link), Play fallback if app missing. Syntax: developer.chrome.com/docs/android/intents.
func intentURL(host, key string, q url.Values, pkg, fallback string) string {
	short := httpx.AddParams("https://"+host+"/"+key, q, func(k, v string) bool { return v != "" && !internalParams[k] })
	return "intent://" + strings.TrimPrefix(short, "https://") +
		"#Intent;scheme=https;package=" + pkg + ";S.browser_fallback_url=" + url.QueryEscape(fallback) + ";end"
}

// schemeURL: app route -> <scheme>://<route> (SDK custom-scheme branch reads host+path+query back as the route).
func schemeURL(scheme, route string) string {
	return scheme + "://" + strings.TrimPrefix(route, "/")
}

// inAppMenu: per-app menu label that differs from the generic "Open in browser".
var inAppMenu = map[string]string{
	"instagram": "Open in external browser",
	"threads":   "Open in external browser",
}

// tapSteps: numbered "Open in browser" steps for source app + platform.
func tapSteps(source string, ios bool) []string {
	icon := "⋮"
	if ios {
		icon = "···"
	}
	return []string{"Tap " + icon + " in the top corner.", "Choose “" + menuLabel(source) + "”."}
}

// menuLabel: app's own menu item, else generic "Open in browser" (any platform).
func menuLabel(source string) string {
	if l, ok := inAppMenu[source]; ok {
		return l
	}
	return "Open in browser"
}

// serveTap: tap page, plain link to tap (in-app browsers block auto store hand-off). iOS copies short link first (pasteboard signal), reports it like copy page; app scheme set -> tries the installed app before the App Store.
func (p *pipelineServer) serveTap(w http.ResponseWriter, r *http.Request, link store.Link, dest, source string, q url.Values) {
	ios := ua.IsIOS(r.UserAgent())
	data := struct {
		Href  template.URL
		Play  template.URL
		IOS   bool
		Steps []string
		Paste string
		Open  template.URL // iOS: app scheme URL tried before Href (App Store)
		Icon  string
	}{Href: template.URL(dest), IOS: ios, Steps: tapSteps(source, ios)}
	app, err := p.st.GetApp(link.AppID)
	if err != nil {
		p.log.Printf("tap page app lookup failed (plain store button, icon from link URLs only): %v", err)
		app = store.App{}
	}
	data.Icon = appIcons.first(iconKeys(link.IOS, link.Android, app.AndroidPackage))
	if ios {
		data.Paste = store.PasteQuery() // reported with pasted_link (pasteToken)
		// app scheme set -> button opens the installed app at the route first, App Store after a timeout
		if app.IOSScheme != "" && httpx.IsRoute(link.URL) {
			data.Open = template.URL(schemeURL(app.IOSScheme, deepLinkURL(link, q)))
		}
	} else if app.AndroidPackage != "" {
		data.Href = template.URL(intentURL(r.Host, link.Key, q, app.AndroidPackage, dest))
		data.Play = template.URL(dest)
	}
	htmlNoStore(w)
	_ = tapTmpl.Execute(w, data)
}

// tapTmpl: centered; store icon (fallback glyph), one primary button (plain href, no JS needed), Android-only Play text link, numbered steps. 48px full-width button, light + dark.
var tapTmpl = template.Must(template.New("tap").Parse(`<!doctype html>
<meta charset="utf-8"><meta name="robots" content="noindex"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Get the app</title>
<style>
:root{color-scheme:light dark;--bg:#fff;--fg:#111;--muted:#767676;--btn:#3a3a3c;--btn-fg:#fff;--ph:#ececec;--ph-fg:#b5b5b5}
@media (prefers-color-scheme:dark){:root{--bg:#111;--fg:#f2f2f2;--muted:#999;--btn:#d1d1d6;--btn-fg:#111;--ph:#2a2a2a;--ph-fg:#5c5c5c}}
body{margin:0;background:var(--bg);color:var(--fg);font:17px/1.45 -apple-system,system-ui,sans-serif}
main{box-sizing:border-box;max-width:28rem;min-height:100vh;min-height:100dvh;margin:0 auto;padding:48px 16px;display:flex;flex-direction:column;justify-content:center;gap:20px;text-align:center}
.go{display:block;min-height:48px;box-sizing:border-box;padding:14px 20px;border-radius:12px;background:var(--btn);color:var(--btn-fg);font-weight:600;text-decoration:none}
.alt{color:var(--fg)}
p,ol{margin:0;color:var(--muted);font-size:13px}
ol{align-self:center;text-align:left;padding-left:1.4em;display:grid;gap:4px;margin-top:-12px}
.icon{align-self:center;width:80px;height:80px;border-radius:18px;margin-bottom:4px;object-fit:cover;box-shadow:0 0 0 1px rgba(127,127,127,.2)}
svg.icon{background:var(--ph);color:var(--ph-fg)}
</style>
<main>
{{if .Icon}}<img class="icon" src="{{.Icon}}" alt="" width="80" height="80">{{else}}<svg class="icon" viewBox="0 0 80 80" aria-hidden="true"><g fill="currentColor"><rect x="22" y="22" width="15" height="15" rx="4"/><rect x="43" y="22" width="15" height="15" rx="4"/><rect x="22" y="43" width="15" height="15" rx="4"/><rect x="43" y="43" width="15" height="15" rx="4"/></g></svg>{{end}}
<a class="go" id="go" href="{{.Href}}"{{if .Open}} data-open="{{.Open}}"{{end}}>Get the app</a>
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
var go = document.getElementById("go");
go.addEventListener("click", function (e) {
  var link = location.origin + location.pathname + {{.Paste}};
  if (copy(link)) {
    var q = new URLSearchParams(location.search);
    q.set("pasted_link", link);
    try { fetch(location.pathname + "?" + q.toString(), { keepalive: true, credentials: "omit" }).catch(function () {}); } catch (e) {}
  }
  // app scheme first; still visible after 1.5s = app missing -> App Store (href). Hidden/pagehide or late timer (frozen while the app was up) cancels
  var open = go.getAttribute("data-open");
  if (!open) return;
  e.preventDefault();
  var left = false, t0 = Date.now();
  document.addEventListener("visibilitychange", function () { if (document.visibilityState === "hidden") left = true; });
  window.addEventListener("pagehide", function () { left = true; });
  setTimeout(function () {
    if (left || document.visibilityState !== "visible" || Date.now() - t0 > 2500) return;
    location.href = go.href;
  }, 1500);
  location.href = open;
});
</script>{{end}}`))

// copyJS: shared by copy + tap pages. true only when execCommand confirms copy synchronously -> pasted_link never reported for a failed copy. Clipboard API = best-effort extra: its promise can't confirm before tap navigates.
const copyJS = `function copy(text) {
  var t = document.createElement("textarea"), ok = false;
  t.value = text; t.setAttribute("readonly", ""); t.style.position = "fixed"; t.style.opacity = "0";
  document.body.appendChild(t); t.select(); t.setSelectionRange(0, text.length);
  try { ok = document.execCommand("copy"); } catch (e) {}
  document.body.removeChild(t);
  if (!ok && navigator.clipboard && navigator.clipboard.writeText) {
    try { navigator.clipboard.writeText(text).catch(function () {}); } catch (e) {}
  }
  // iOS app paste guard (hasURLs) sees a URL type, not plain text: re-write as text/uri-list; reject = plain copy stays
  if (ok && window.ClipboardItem && navigator.clipboard && navigator.clipboard.write) {
    try {
      navigator.clipboard.write([new ClipboardItem({
        "text/uri-list": new Blob([text], { type: "text/uri-list" }),
        "text/plain": new Blob([text], { type: "text/plain" })
      })]).catch(function () {});
    } catch (e) {}
  }
  return ok;
}`
