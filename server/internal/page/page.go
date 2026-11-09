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

const MaxHTML = 51200

type Placeholder struct {
	Name string `json:"name"`
	Desc string `json:"desc"`
}

var Placeholders = []Placeholder{
	{"cta", "Button `.detur-cta`. Required, unless an element has `id=\"detur-cta\"`"},
	{"cta_url", "Button link"},
	{"icon_url", "Store icon"},
	{"app_name", "App name"},
	{"play_link", "Play Store link `.detur-alt`, Android in-app only"},
	{"steps", "\"Open in browser\" steps `.detur-hint` `.detur-steps`, in-app only"},
	{"web_link", "\"Continue on web\" link `#detur-web`, shown after the visitor returns from the store"},
}

const DefaultHTML = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width,initial-scale=1">
  <meta name="robots" content="noindex">
  <title>Get the app</title>
  <style>
    :root { color-scheme: light dark; }
    body {
      margin: 0;
      font: 17px/1.45 system-ui, sans-serif;
      text-align: center;
    }
    main {
      max-width: 28rem;
      min-height: 100dvh;
      margin: auto;
      padding: 0 16px;
      display: flex;
      flex-direction: column;
      justify-content: center;
      gap: 20px;
    }
    .icon {
      align-self: center;
      width: 80px;
      height: 80px;
      border-radius: 18px;
    }
    .detur-cta {
      padding: 14px 20px;
      border-radius: 12px;
      background: CanvasText;
      color: Canvas;
      font-weight: 600;
      text-decoration: none;
    }
    .detur-alt { color: inherit; }
    .detur-hint, .detur-steps {
      margin: 0;
      color: GrayText;
      font-size: 13px;
    }
    .detur-steps {
      align-self: center;
      margin-top: -12px;
      padding-left: 1.4em;
      text-align: left;
    }
  </style>
</head>
<body>
  <main>
    <img class="icon" src="{{icon_url}}" alt="">
    {{cta}}
    {{play_link}}
    {{web_link}}
    {{steps}}
  </main>
</body>
</html>
`

var glyphIcon = html.EscapeString("data:image/svg+xml," + url.PathEscape(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 80 80"><rect width="80" height="80" fill="#7f7f7f" fill-opacity=".2"/><g fill="#7f7f7f" fill-opacity=".6"><rect x="22" y="22" width="15" height="15" rx="4"/><rect x="43" y="22" width="15" height="15" rx="4"/><rect x="22" y="43" width="15" height="15" rx="4"/><rect x="43" y="43" width="15" height="15" rx="4"/></g></svg>`))

type Vars struct{ CTA, CTAURL, PlayLink, WebLink, IconURL, AppName, Steps string }

func Render(page string, v Vars) string {
	return strings.NewReplacer(
		"{{cta}}", v.CTA, "{{cta_url}}", v.CTAURL, "{{play_link}}", v.PlayLink, "{{web_link}}", v.WebLink,
		"{{icon_url}}", v.IconURL, "{{app_name}}", v.AppName, "{{steps}}", v.Steps,
	).Replace(page)
}

var rePlaceholder = regexp.MustCompile(`\{\{[^{}]*\}\}`)

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

type Input struct {
	Copy    bool
	IOS     bool
	Href    string
	Open    string
	Play    string
	Web     string
	Steps   []string
	Icon    string
	AppName string
	Paste   string
	Delay   int
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
	if in.Web != "" {
		v.WebLink = `<a id="detur-web" class="detur-alt" href="` + html.EscapeString(in.Web) + `" style="display:none">Continue on web</a>`
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
