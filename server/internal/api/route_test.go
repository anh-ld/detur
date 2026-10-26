package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// Route destination -> /<first 3 of appID><route> path on both SDK responses (SDK drops the first segment). Full URLs unchanged.
func TestRouteDestinationExpanded(t *testing.T) {
	ts, s, _ := newTestServer(t)
	app, link := setup(t, s)
	link.URL = "/product/42?ref=sim"
	if err := s.UpdateLink(link); err != nil {
		t.Fatal(err)
	}
	want := "/" + app.ID[:3] + "/product/42?ref=sim"

	_, b := doPost(t, ts, "/api/link/resolve-short", `{"url":"https://lnk.example/abc"}`, authHeaders(app.ID))
	var out struct{ Link string }
	if err := json.Unmarshal(b, &out); err != nil || out.Link != want {
		t.Fatalf("resolve-short link = %s; want %s", b, want)
	}

	c := recordAndroidClick(t, s, app, link)
	resp, b := doPost(t, ts, "/api/link/match-link", `{"clickId":"`+c.ID+`"}`, authHeaders(app.ID))
	if err := json.Unmarshal(b, &out); err != nil || resp.StatusCode != http.StatusOK || out.Link != want {
		t.Fatalf("match-link = %d %s; want %s", resp.StatusCode, b, want)
	}
}

func TestURLDestinationUnchanged(t *testing.T) {
	ts, s, _ := newTestServer(t)
	app, link := setup(t, s)
	_, b := doPost(t, ts, "/api/link/resolve-short", `{"url":"https://lnk.example/abc"}`, authHeaders(app.ID))
	if !strings.Contains(string(b), `"link":"`+link.URL+`"`) {
		t.Fatalf("resolve-short = %s; want %s unchanged", b, link.URL)
	}
}

// Portal: url accepts an app route; bare paths and protocol-relative URLs stay rejected.
func TestPortalRouteDestinationValidation(t *testing.T) {
	portal, _, _ := newPortalEnv(t)
	_, b := portalReq(t, portal, "POST", "/api/apps", `{"name":"routes"}`, nil)
	var app struct{ ID string }
	mustJSON(t, b, &app)
	for i, c := range []struct {
		url  string
		want int
	}{
		{"/product/42?ref=x", http.StatusCreated},
		{"product/42", http.StatusBadRequest},
		{"//evil.example/x", http.StatusBadRequest},
		{"/", http.StatusCreated},
		{"https://example.com/product/42", http.StatusBadRequest},
		{"myapp://product/42", http.StatusBadRequest},
	} {
		body := `{"key":"r` + string(rune('a'+i)) + `","url":"` + c.url + `"}`
		if resp, rb := portalReq(t, portal, "POST", "/api/apps/"+app.ID+"/links", body, nil); resp.StatusCode != c.want {
			t.Errorf("url %q: %d %s; want %d", c.url, resp.StatusCode, rb, c.want)
		}
	}
}

// Update path takes the same destination rules as create.
func TestPortalRouteDestinationUpdate(t *testing.T) {
	portal, _, _ := newPortalEnv(t)
	_, b := portalReq(t, portal, "POST", "/api/apps", `{"name":"routes"}`, nil)
	var app struct{ ID string }
	mustJSON(t, b, &app)
	_, b = portalReq(t, portal, "POST", "/api/apps/"+app.ID+"/links", `{"key":"up","url":"/home"}`, nil)
	var link struct{ ID string }
	mustJSON(t, b, &link)
	for _, c := range []struct {
		url  string
		want int
	}{{"/product/42", http.StatusOK}, {"product/42", http.StatusBadRequest}} {
		if resp, rb := portalReq(t, portal, "PATCH", "/api/links/"+link.ID, `{"url":"`+c.url+`"}`, nil); resp.StatusCode != c.want {
			t.Errorf("update url %q: %d %s; want %d", c.url, resp.StatusCode, rb, c.want)
		}
	}
}

func TestAppDestination(t *testing.T) {
	for _, c := range []struct{ appID, dest, want string }{
		{"CUmUAFfB", "/product/42?x=1", "/CUm/product/42?x=1"},
		{"CUmUAFfB", "/", "/CUm/"},
		{"ab", "/p", "/ab/p"},
		{"CUmUAFfB", "https://example.com/p", "https://example.com/p"},
		{"CUmUAFfB", "myapp://p", "myapp://p"},
	} {
		if got := appDestination(c.appID, c.dest); got != c.want {
			t.Errorf("appDestination(%q, %q) = %q; want %q", c.appID, c.dest, got, c.want)
		}
	}
}
