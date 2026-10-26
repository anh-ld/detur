package api

import (
	"net/http"
	"strings"
	"testing"
)

// Social preview fields: create, round-trip, limits, https-only image.
func TestPortalLinkOG(t *testing.T) {
	portal, _, _ := newPortalEnv(t)
	_, b := portalReq(t, portal, "POST", "/api/apps", `{"name":"og"}`, nil)
	var app struct {
		ID string `json:"id"`
	}
	mustJSON(t, b, &app)
	path := "/api/apps/" + app.ID + "/links"
	for i, c := range []struct {
		body string
		want int
	}{
		{`{"key":"o1","url":"https://example.com","ogImage":"http://cdn.example/a.png"}`, http.StatusBadRequest},
		{`{"key":"o2","url":"https://example.com","ogImage":"javascript:alert(1)"}`, http.StatusBadRequest},
		{`{"key":"o3","url":"https://example.com","ogTitle":"` + strings.Repeat("é", 101) + `"}`, http.StatusBadRequest},
		{`{"key":"o4","url":"https://example.com","ogDescription":"` + strings.Repeat("x", 301) + `"}`, http.StatusBadRequest},
		{`{"key":"o5","url":"https://example.com","ogTitle":" Hi ","ogDescription":"d","ogImage":"https://cdn.example/a.png"}`, http.StatusCreated},
	} {
		if resp, rb := portalReq(t, portal, "POST", path, c.body, nil); resp.StatusCode != c.want {
			t.Errorf("case %d: %d %s; want %d", i, resp.StatusCode, rb, c.want)
		}
	}
	_, b = portalReq(t, portal, "GET", path, "", nil)
	var links []struct {
		ID, OGTitle, OGImage string
	}
	mustJSON(t, b, &links)
	if len(links) != 1 || links[0].OGTitle != "Hi" || links[0].OGImage != "https://cdn.example/a.png" {
		t.Fatalf("links = %+v; want trimmed preview fields", links)
	}
	if resp, rb := portalReq(t, portal, "PATCH", "/api/links/"+links[0].ID, `{"ogImage":"http://x"}`, nil); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("update bad ogImage: %d %s; want 400", resp.StatusCode, rb)
	}
	if resp, rb := portalReq(t, portal, "PATCH", "/api/links/"+links[0].ID, `{"ogTitle":""}`, nil); resp.StatusCode != http.StatusOK {
		t.Errorf("clear ogTitle: %d %s; want 200", resp.StatusCode, rb)
	}
}
