package pipeline

// Get-app page serving: copy page (iOS hop 1) + tap page (in-app hop 2). Content + placeholders: internal/page.

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	stdhtml "html"
	"net/http"
	"strings"

	"detur.dev/server/internal/page"
)

// scripts: button behavior appended to every page (nonce'd). URLs come from JS vars, not el.href -> `<button id="detur-cta">` works.
func pageScripts(in page.Input, nonce string) string {
	js := func(v any) string { b, _ := json.Marshal(v); return string(b) } // escapes < > &
	var b strings.Builder
	b.WriteString(`<script nonce="` + nonce + `">` + "\n" + `var HREF = ` + js(in.Href) + `, cta = document.getElementById("detur-cta");` + "\n")
	switch {
	case in.Copy:
		b.WriteString(`var PASTE = ` + js(in.Paste) + `, delay = ` + js(in.Delay) + ", left = false;\n" + reloadJS + "\n" + copyJS + `
if (cta) cta.addEventListener("click", function (e) {
  e.preventDefault();
  var link = location.origin + location.pathname + PASTE;
  go(copy(link) ? link : "");
});`)
	default:
		// drop the safety-net flag from this page's URL: "Open in browser" then reopens the plain link (real browser -> store redirect)
		b.WriteString(`(function () {
  var q = new URLSearchParams(location.search);
  if (!q.has("_tap")) return;
  q.delete("_tap");
  try { history.replaceState(null, "", location.pathname + "?" + q.toString()); } catch (e) {}
})();
`)
		if !in.IOS {
			b.WriteString(`if (cta && !cta.getAttribute("href")) cta.addEventListener("click", function () { location.href = HREF; });`)
			break
		}
		// copy synchronously inside the tap, report pasted_link before the navigation can unload the page
		b.WriteString(`var OPEN = ` + js(in.Open) + `, PASTE = ` + js(in.Paste) + ";\n" + copyJS + `
if (cta) cta.addEventListener("click", function (e) {
  var link = location.origin + location.pathname + PASTE;
  if (copy(link)) {
    var q = new URLSearchParams(location.search);
    q.set("pasted_link", link);
    try { fetch(location.pathname + "?" + q.toString(), { keepalive: true, credentials: "omit" }).catch(function () {}); } catch (e) {}
  }
  // app scheme first; still visible after 1.5s = app missing -> App Store (HREF). Hidden/pagehide or late timer (frozen while the app was up) cancels
  if (!OPEN) { if (!cta.getAttribute("href")) { e.preventDefault(); location.href = HREF; } return; }
  e.preventDefault();
  var left = false, t0 = Date.now();
  document.addEventListener("visibilitychange", function () { if (document.visibilityState === "hidden") left = true; });
  window.addEventListener("pagehide", function () { left = true; });
  setTimeout(function () {
    if (left || document.visibilityState !== "visible" || Date.now() - t0 > 2500) return;
    location.href = HREF;
  }, 1500);
  location.href = OPEN;
});`)
	}
	b.WriteString("\n</script>")
	if in.Copy {
		// no-JS visitors still reach hop 2
		return `<noscript><meta http-equiv="refresh" content="0;url=` + stdhtml.EscapeString(in.Href) + `"></noscript>` + b.String()
	}
	return b.String()
}

// withScripts: add before the last </body>, else at the end.
func withScripts(doc, add string) string {
	if i := strings.LastIndex(strings.ToLower(doc), "</body>"); i >= 0 {
		return doc[:i] + add + doc[i:]
	}
	return doc + add
}

// servePage: render + CSP (nonce per response) + scripts. Custom HTML can't run script, only ours.
func servePage(w http.ResponseWriter, html string, in page.Input) {
	n := make([]byte, 16)
	_, _ = rand.Read(n)
	nonce := hex.EncodeToString(n)
	htmlNoStore(w)
	w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'nonce-"+nonce+"'; style-src 'unsafe-inline' https:; "+
		"img-src https: data:; font-src https: data:; media-src https:; connect-src 'self'; base-uri 'none'; form-action 'none'")
	if in.Copy {
		w.Header().Set("Accept-CH", "Sec-CH-UA-Model, Sec-CH-UA-Platform-Version")
	}
	if html == "" {
		html = page.DefaultHTML
	}
	_, _ = w.Write([]byte(withScripts(page.Render(html, in.Vars()), pageScripts(in, nonce))))
}
