package page

import (
	"strings"
	"testing"
)

// Single pass: app name "{{cta}}" stays text, markup in names is escaped, no icon -> data-URI glyph.
func TestRenderSinglePass(t *testing.T) {
	out := Preview("<p>{{app_name}}</p><img src=\"{{icon_url}}\">{{cta}}", "tap", "ios", "{{cta}} <b>x</b>")
	if !strings.Contains(out, "<p>{{cta}} &lt;b&gt;x&lt;/b&gt;</p>") || strings.Count(out, `id="detur-cta"`) != 1 {
		t.Errorf("render = %s", out)
	}
	if !strings.Contains(out, `src="data:image/svg+xml,`) {
		t.Errorf("no glyph icon: %s", out)
	}
}

func TestValidate(t *testing.T) {
	for _, c := range []struct{ name, html, want string }{
		{"empty resets", "", ""},
		{"default ok", DefaultHTML, ""},
		{"own element ok", `<button id="detur-cta">Go</button>`, ""},
		{"too big", "{{cta}}" + strings.Repeat("a", MaxHTML), "longer than"},
		{"script", "{{cta}}<SCRIPT>1</SCRIPT>", "scripts"},
		{"no button", "<p>hi</p>", "needs the button"},
		{"typo", "{{cta}}{{icon}}", "unknown placeholder {{icon}}"},
	} {
		err := Validate(c.html)
		if (c.want == "") != (err == nil) || (err != nil && !strings.Contains(err.Error(), c.want)) {
			t.Errorf("%s: err = %v; want %q", c.name, err, c.want)
		}
	}
}
