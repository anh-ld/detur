package pipeline

import (
	"regexp"
	"strings"
	"testing"

	"detur.dev/server/internal/store"
)

func TestCustomPageServed(t *testing.T) {
	ts, s, _ := newPipelineServer(t)
	app, link := setupPipeline(t, s)
	if err := s.UpdateAppDetails(app.ID, "", "com.example", "AA:BB", ""); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateAppPage(app.ID, `<!doctype html><body><h1>Hi {{app_name}}</h1><button id="detur-cta">Go</button>{{play_link}}{{steps}}</body>`); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name, path, ua string
		want           []string
	}{
		{"ios copy", "/" + link.Key, iosUA, []string{"<h1>Hi pipeline test app</h1>", `<noscript><meta http-equiv="refresh" content="0;url=?_dt=1">`, "go(copy(link)"}},
		{"ios tap", "/" + link.Key + "?_dt=1", messengerIOSUA, []string{"Open in browser", "pasted_link"}},
		{"android tap", "/" + link.Key + "?_dt=1", messengerAndroidUA, []string{"Open Play Store", "var HREF = \"intent://"}},
	} {
		resp, body := doGET(t, ts, c.path, map[string]string{"User-Agent": c.ua})
		page := string(body)
		for _, w := range append(c.want, `<button id="detur-cta">Go</button>`, `getElementById("detur-cta")`) {
			if !strings.Contains(page, w) {
				t.Errorf("%s: missing %q:\n%s", c.name, w, page)
			}
		}
		csp := resp.Header.Get("Content-Security-Policy")
		m := regexp.MustCompile(`script-src 'nonce-([0-9a-f]{32})'`).FindStringSubmatch(csp)
		if m == nil || !strings.Contains(csp, "connect-src 'self'") {
			t.Fatalf("%s: CSP = %q", c.name, csp)
		}
		if n := strings.Count(page, "<script"); n != strings.Count(page, `<script nonce="`+m[1]+`">`) || n == 0 {
			t.Errorf("%s: %d scripts, nonce'd %d", c.name, n, strings.Count(page, `<script nonce="`+m[1]+`">`))
		}
	}
}

func TestDefaultCopyPageDesign(t *testing.T) {
	ts, s, _ := newPipelineServer(t)
	_, link := setupPipeline(t, s)
	resp, body := doGET(t, ts, "/"+link.Key, map[string]string{"User-Agent": iosUA})
	for _, w := range []string{`class="icon"`, `id="detur-cta" class="detur-cta" href="?_dt=1"`, "Open in App Store"} {
		if !strings.Contains(string(body), w) {
			t.Errorf("copy page missing %q:\n%s", w, body)
		}
	}
	if resp.Header.Get("Content-Security-Policy") == "" {
		t.Error("copy page without CSP")
	}
}

func TestPageUserScriptNeverNonced(t *testing.T) {
	ts, s, _ := newPipelineServer(t)
	app, link := setupPipeline(t, s)
	if err := s.UpdateAppPage(app.ID, `<body><script>steal()</script><div onclick="x()">{{cta}}</div></body>`); err != nil {
		t.Fatal(err)
	}
	resp, body := mobileClick(t, ts, "/"+link.Key, map[string]string{"User-Agent": messengerIOSUA})
	if !strings.Contains(string(body), "<script>steal()</script>") {
		t.Fatalf("user script rewritten, test premise broken:\n%s", body)
	}
	if csp := resp.Header.Get("Content-Security-Policy"); !regexp.MustCompile(`script-src 'nonce-[0-9a-f]{32}'; `).MatchString(csp) {
		t.Errorf("script-src not nonce-only: %q", csp)
	}
}

func TestPageHostileQueryEscaped(t *testing.T) {
	ts, s, _ := newPipelineServer(t)
	app, link := setupPipeline(t, s)
	if err := s.UpdateAppDetails(app.ID, "", "com.example", "AA:BB", ""); err != nil {
		t.Fatal(err)
	}
	evil := "?ct=%3C%2Fscript%3E%3Cscript%3Ealert(1)%3C%2Fscript%3E&utm_source=%22%3E%3Cimg%20src%3Dx%3E"
	for _, ua := range []string{messengerIOSUA, messengerAndroidUA} {
		_, body := mobileClick(t, ts, "/"+link.Key+evil, map[string]string{"User-Agent": ua})
		page := string(body)
		if !strings.Contains(page, "alert") {
			t.Fatalf("%s: payload never reached the page, test premise broken:\n%s", ua, page)
		}
		for _, bad := range []string{"<script>alert(1)", `"><img src=x>`} {
			if strings.Contains(page, bad) {
				t.Errorf("%s: raw %q in page:\n%s", ua, bad, page)
			}
		}
	}
}

func TestPageCustomButtonSchemeFirst(t *testing.T) {
	ts, s, _ := newPipelineServer(t)
	app, _ := setupPipeline(t, s)
	if err := s.UpdateAppDetails(app.ID, "", "", "", "myapp"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateLink(store.Link{AppID: app.ID, Key: "invite", URL: "/invite", IOS: "https://apps.apple.com/app/id123"}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateAppPage(app.ID, `<button id="detur-cta">Open</button>`); err != nil {
		t.Fatal(err)
	}
	_, body := mobileClick(t, ts, "/invite?userId=1", map[string]string{"User-Agent": messengerIOSUA})
	for _, w := range []string{`var OPEN = "myapp://invite?userId=1"`, `var HREF = "https://apps.apple.com/app/id123"`, `<button id="detur-cta">Open</button>`} {
		if !strings.Contains(string(body), w) {
			t.Errorf("missing %q:\n%s", w, body)
		}
	}
}

func TestPageNotOnAutoInterstitial(t *testing.T) {
	ts, s, _ := newPipelineServer(t)
	app, link := setupPipeline(t, s)
	if err := s.UpdateAppPage(app.ID, `<h1>custom</h1>{{cta}}`); err != nil {
		t.Fatal(err)
	}
	_, body := doGET(t, ts, "/"+link.Key, map[string]string{"User-Agent": androidUA})
	if strings.Contains(string(body), "<h1>custom</h1>") {
		t.Errorf("custom page on auto interstitial:\n%s", body)
	}
}

func TestWithScripts(t *testing.T) {
	for _, c := range [][3]string{
		{"<p>a</p>", "<s>", "<p>a</p><s>"},
		{"<body>x</BODY></html>", "<s>", "<body>x<s></BODY></html>"},
		{"</body><p>tail</p></body>", "<s>", "</body><p>tail</p><s></body>"},
	} {
		if got := withScripts(c[0], c[1]); got != c[2] {
			t.Errorf("withScripts(%q) = %q; want %q", c[0], got, c[2])
		}
	}
}
