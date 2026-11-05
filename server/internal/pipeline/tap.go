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

// serveTap: tap page (get-app page, page.go), plain link to tap (in-app browsers block auto store hand-off). iOS copies short link first (pasteboard signal), reports it like copy page; app scheme set -> tries the installed app before the App Store.
func (p *pipelineServer) serveTap(w http.ResponseWriter, r *http.Request, link store.Link, dest, source string, q url.Values) {
	ios := ua.IsIOS(r.UserAgent())
	in := page.Input{IOS: ios, Href: dest, Steps: tapSteps(source, ios)}
	app, err := p.st.GetApp(link.AppID)
	if err != nil {
		p.log.Printf("tap page app lookup failed (default page, icon from link URLs only): %v", err)
		app = store.App{}
	}
	in.AppName = app.Name
	in.Icon = appIcons.first(iconKeys(link.IOS, link.Android, app.AndroidPackage))
	if ios {
		in.Paste = store.PasteQuery() // reported with pasted_link (pasteToken)
		// app scheme set -> button opens the installed app at the route first, App Store after a timeout
		if app.IOSScheme != "" && httpx.IsRoute(link.URL) {
			in.Open = schemeURL(app.IOSScheme, deepLinkURL(link, q))
		}
	} else if app.AndroidPackage != "" {
		in.Href = intentURL(r.Host, link.Key, q, app.AndroidPackage, dest)
		in.Play = dest
	}
	servePage(w, app.PageHTML, in)
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
