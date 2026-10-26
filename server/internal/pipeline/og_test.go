package pipeline

import (
	"net/http"
	"strings"
	"testing"

	"detur.dev/server/internal/store"
)

// Crawler + preview set: 200 OG page, escaped, still pointing on to the 302 target.
func TestOGPageForCrawlers(t *testing.T) {
	ts, s, _ := newPipelineServer(t)
	_, link := setupPipeline(t, s)
	link.OGTitle, link.OGDescription, link.OGImage = `Sam <invited> you`, "Get 20% off", "https://cdn.example/card.png"
	if err := s.UpdateLink(link); err != nil {
		t.Fatal(err)
	}
	for _, agent := range []string{"facebookexternalhit/1.1", "Twitterbot/1.0", "WhatsApp/2.23.20.0"} {
		resp, body := doGET(t, ts, "/"+link.Key, map[string]string{"User-Agent": agent})
		page := string(body)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s: status = %d; want 200", agent, resp.StatusCode)
		}
		for _, want := range []string{`og:title" content="Sam &lt;invited&gt; you"`, `og:image" content="https://cdn.example/card.png"`,
			`summary_large_image`, `href="` + link.FallbackURL + `"`, `location.replace("` + link.FallbackURL + `")`} {
			if !strings.Contains(page, want) {
				t.Errorf("%s: page missing %q:\n%s", agent, want, page)
			}
		}
	}
}

// No preview set -> crawler keeps the 302; people never see the OG page.
func TestOGPageOnlyWhenSet(t *testing.T) {
	ts, s, _ := newPipelineServer(t)
	_, link := setupPipeline(t, s)
	if resp, _ := doGET(t, ts, "/"+link.Key, map[string]string{"User-Agent": "facebookexternalhit/1.1"}); resp.StatusCode != http.StatusFound {
		t.Errorf("no preview: status = %d; want 302", resp.StatusCode)
	}
	link.OGTitle = "Hi"
	if err := s.UpdateLink(link); err != nil {
		t.Fatal(err)
	}
	if resp, _ := doGET(t, ts, "/"+link.Key, map[string]string{"User-Agent": desktopUA}); resp.StatusCode != http.StatusFound {
		t.Errorf("person: status = %d; want 302", resp.StatusCode)
	}
}

// OG page for a route destination: preview only, no Continue/redirect to a relative path.
func TestOGPageRouteDestination(t *testing.T) {
	ts, s, _ := newPipelineServer(t)
	app, _ := setupPipeline(t, s)
	if _, err := s.CreateLink(store.Link{AppID: app.ID, Key: "ogr", URL: "/product/42", OGTitle: "Hi"}); err != nil {
		t.Fatal(err)
	}
	_, body := doGET(t, ts, "/ogr", map[string]string{"User-Agent": "facebookexternalhit/1.1"})
	page := string(body)
	if !strings.Contains(page, `og:title" content="Hi"`) || strings.Contains(page, "Continue") || strings.Contains(page, "location.replace") {
		t.Errorf("route OG page: want preview without redirect:\n%s", page)
	}
}
