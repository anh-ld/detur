// Package page: get-app page content (copy page iOS hop 1, tap page in-app hop 2). One HTML per app (apps.page_html), "" = default.
// Placeholders swapped in a single pass; the button script is ours (pipeline), never user-written.
package page

import (
	"fmt"
	"html"
	"net/url"
	"regexp"
	"strings"
	"unicode/utf8"
)

// MaxHTML: saved page limit (Detour: 50 KB).
const MaxHTML = 51200

// Placeholder: name + one-line meaning, shown in the portal editor.
type Placeholder struct {
	Name string `json:"name"`
	Desc string `json:"desc"`
}

// Placeholders: the only {{names}} a page may use.
var Placeholders = []Placeholder{
	{"cta", "The button (required unless an element has id=\"detur-cta\")"},
	{"cta_url", "Button target URL (store, intent or next hop)"},
	{"icon_url", "Store icon URL, else a placeholder glyph"},
	{"app_name", "App name from the portal"},
	{"play_link", "Android tap page: Play Store text link, else empty"},
	{"steps", "Tap page: \"open in browser\" steps, else empty"},
}

// DefaultHTML: centered; store icon, one full-width 48px button, Android Play text link, numbered steps. Light + dark.
const DefaultHTML = `<!doctype html>
<meta charset="utf-8"><meta name="robots" content="noindex"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Get the app</title>
<style>
:root{color-scheme:light dark;--bg:#fff;--fg:#111;--muted:#767676;--btn:#3a3a3c;--btn-fg:#fff}
@media (prefers-color-scheme:dark){:root{--bg:#111;--fg:#f2f2f2;--muted:#999;--btn:#d1d1d6;--btn-fg:#111}}
body{margin:0;background:var(--bg);color:var(--fg);font:17px/1.45 -apple-system,system-ui,sans-serif}
main{box-sizing:border-box;max-width:28rem;min-height:100vh;min-height:100dvh;margin:0 auto;padding:48px 16px;display:flex;flex-direction:column;justify-content:center;gap:20px;text-align:center}
.detur-cta{display:block;min-height:48px;box-sizing:border-box;padding:14px 20px;border-radius:12px;background:var(--btn);color:var(--btn-fg);font-weight:600;text-decoration:none}
.detur-alt{color:var(--fg)}
.detur-hint,.detur-steps{margin:0;color:var(--muted);font-size:13px}
.detur-steps{align-self:center;text-align:left;padding-left:1.4em;display:grid;gap:4px;margin-top:-12px}
.icon{align-self:center;width:80px;height:80px;border-radius:18px;margin-bottom:4px;object-fit:cover;box-shadow:0 0 0 1px rgba(127,127,127,.2)}
</style>
<main>
<img class="icon" src="{{icon_url}}" alt="" width="80" height="80">
{{cta}}
{{play_link}}
{{steps}}
</main>
`

// glyphIcon: data-URI stand-in when no store icon is known (never a broken <img>). Translucent grey works on light + dark.
var glyphIcon = html.EscapeString("data:image/svg+xml," + url.PathEscape(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 80 80"><rect width="80" height="80" fill="#7f7f7f" fill-opacity=".2"/><g fill="#7f7f7f" fill-opacity=".6"><rect x="22" y="22" width="15" height="15" rx="4"/><rect x="43" y="22" width="15" height="15" rx="4"/><rect x="22" y="43" width="15" height="15" rx="4"/><rect x="43" y="43" width="15" height="15" rx="4"/></g></svg>`))

// Vars: placeholder values, already HTML-safe.
type Vars struct{ CTA, CTAURL, PlayLink, IconURL, AppName, Steps string }

// Render: single pass; values never re-scanned -> app name "{{cta}}" stays text.
func Render(page string, v Vars) string {
	return strings.NewReplacer(
		"{{cta}}", v.CTA, "{{cta_url}}", v.CTAURL, "{{play_link}}", v.PlayLink,
		"{{icon_url}}", v.IconURL, "{{app_name}}", v.AppName, "{{steps}}", v.Steps,
	).Replace(page)
}

var rePlaceholder = regexp.MustCompile(`\{\{[^{}]*\}\}`)

// Validate: save-time checks, message goes straight to the portal. "" (default) always ok.
func Validate(page string) error {
	lower := strings.ToLower(page)
	switch {
	case page == "":
		return nil
	case len(page) > MaxHTML:
		return fmt.Errorf("page longer than %d bytes", MaxHTML)
	case !utf8.ValidString(page):
		return fmt.Errorf("page is not valid UTF-8")
	case strings.Contains(lower, "<script"):
		return fmt.Errorf("scripts are not allowed (the button script is added for you)")
	case !strings.Contains(page, "{{cta}}") && !strings.Contains(lower, `id="detur-cta"`) && !strings.Contains(lower, `id='detur-cta'`):
		return fmt.Errorf("page needs the button: {{cta}} or an element with id=\"detur-cta\"")
	}
	known := map[string]bool{}
	for _, p := range Placeholders {
		known["{{"+p.Name+"}}"] = true
	}
	for _, m := range rePlaceholder.FindAllString(page, -1) {
		if !known[m] {
			names := make([]string, len(Placeholders))
			for i, p := range Placeholders {
				names[i] = "{{" + p.Name + "}}"
			}
			return fmt.Errorf("unknown placeholder %s (known: %s)", m, strings.Join(names, ", "))
		}
	}
	return nil
}

// Input: one rendered page. Copy: iOS hop 1, CTA = next hop. Tap: in-app hop 2, CTA = store / intent.
type Input struct {
	Copy    bool
	IOS     bool
	Href    string   // button target
	Open    string   // tap iOS: app scheme URL tried before Href
	Play    string   // tap Android: Play URL for the text link
	Steps   []string // tap
	Icon    string
	AppName string
	Paste   string // iOS: `_dc` query appended to the copied link
	Delay   int    // copy: safety-net ms
}

func (in Input) Vars() Vars {
	label := "Get the app"
	if in.Copy {
		label = "Open in App Store"
	}
	href := html.EscapeString(in.Href)
	cta := `<a id="detur-cta" class="detur-cta" href="` + href + `"`
	if in.Open != "" {
		cta += ` data-open="` + html.EscapeString(in.Open) + `"`
	}
	v := Vars{CTA: cta + `>` + label + `</a>`, CTAURL: href, IconURL: glyphIcon, AppName: html.EscapeString(in.AppName)}
	if in.Icon != "" {
		v.IconURL = html.EscapeString(in.Icon)
	}
	if in.Play != "" {
		v.PlayLink = `<a class="detur-alt" href="` + html.EscapeString(in.Play) + `">Open Play Store</a>`
	}
	if len(in.Steps) > 0 {
		var b strings.Builder
		b.WriteString(`<p class="detur-hint">Nothing happens? Open this page in your browser:</p><ol class="detur-steps">`)
		for _, s := range in.Steps {
			b.WriteString("<li>" + html.EscapeString(s) + "</li>")
		}
		b.WriteString("</ol>")
		v.Steps = b.String()
	}
	return v
}

// Preview: page as the portal frame shows it (sample links, no scripts). kind "copy" | "tap"; platform "ios" | "android".
func Preview(page, kind, platform, appName string) string {
	if page == "" {
		page = DefaultHTML
	}
	in := Input{Copy: kind == "copy", IOS: platform != "android", Href: "#", AppName: appName}
	if kind != "copy" {
		in.Steps = []string{"Tap ⋮ in the top corner.", "Choose “Open in browser”."}
		if in.IOS {
			in.Steps[0] = "Tap ··· in the top corner."
		} else {
			in.Play = "#"
		}
	}
	return Render(page, in.Vars())
}
