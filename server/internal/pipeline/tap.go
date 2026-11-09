package pipeline

import (
	"net/http"
	"net/url"
	"strings"

	"detur.dev/server/internal/httpx"
	"detur.dev/server/internal/page"
	"detur.dev/server/internal/store"
	"detur.dev/server/internal/ua"
)

func hasStoreTarget(link store.Link, platform string) bool {
	switch platform {
	case ua.PlatformIOS:
		return isAppStoreURL(link.IOS)
	case ua.PlatformAndroid:
		return isPlayStoreURL(link.Android)
	}
	return false
}

func intentURL(host, key string, q url.Values, pkg, fallback string) string {
	short := httpx.AddParams("https://"+host+"/"+key, q, forwardable)
	return "intent://" + strings.TrimPrefix(short, "https://") +
		"#Intent;scheme=https;package=" + pkg + ";S.browser_fallback_url=" + url.QueryEscape(fallback) + ";end"
}

// schemeURL: app route -> <scheme>://<route> (SDK custom-scheme branch reads host+path+query back as the route).
func schemeURL(scheme, route string) string {
	return scheme + "://" + strings.TrimPrefix(route, "/")
}

var inAppMenu = map[string]string{
	"instagram": "Open in external browser",
	"threads":   "Open in external browser",
}

func tapSteps(source string, ios bool) []string {
	icon := "⋮"
	if ios {
		icon = "···"
	}
	return []string{"Tap " + icon + " in the top corner.", "Choose “" + menuLabel(source) + "”."}
}

func menuLabel(source string) string {
	if l, ok := inAppMenu[source]; ok {
		return l
	}
	return "Open in browser"
}

func (p *pipelineServer) serveTap(w http.ResponseWriter, r *http.Request, link store.Link, dest, source string, q url.Values) {
	ios := ua.IsIOS(r.UserAgent())
	in := page.Input{IOS: ios, Href: dest, Steps: tapSteps(source, ios)}
	app := p.appOrDefault(link, "tap page")
	in.AppName = app.Name
	in.Icon = appIcons.first(iconKeys(link.IOS, link.Android, app.AndroidPackage))
	if ios {
		in.Paste = store.PasteQuery()
		if app.IOSScheme != "" && httpx.IsRoute(link.URL) {
			in.Open = schemeURL(app.IOSScheme, deepLinkURL(link, q))
		}
	} else if app.AndroidPackage != "" {
		in.Href = intentURL(r.Host, link.Key, q, app.AndroidPackage, dest)
		in.Play = dest
	}
	servePage(w, app.PageHTML, in)
}

func (p *pipelineServer) appOrDefault(link store.Link, what string) store.App {
	app, err := p.st.GetApp(link.AppID)
	if err != nil {
		p.log.Printf("%s app lookup failed (default page, icon from link URLs only): %v", what, err)
		return store.App{}
	}
	return app
}

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
