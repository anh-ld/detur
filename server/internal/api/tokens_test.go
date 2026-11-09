package api

import (
	"net/http"
	"strings"
	"testing"
)

func TestTokenAdminRoutes(t *testing.T) {
	open, _, _ := newPortalEnv(t)
	for _, ep := range [][2]string{{"GET", "/api/tokens"}, {"POST", "/api/tokens"}, {"PATCH", "/api/tokens/x"}, {"DELETE", "/api/tokens/x"}} {
		resp, b := portalReq(t, open, ep[0], ep[1], `{}`, nil)
		if resp.StatusCode != http.StatusForbidden || !strings.Contains(string(b), "ADMIN_PASSWORD") {
			t.Errorf("%s %s without password = %d %s; want 403 naming ADMIN_PASSWORD", ep[0], ep[1], resp.StatusCode, b)
		}
	}

	portal, _, st := newPortalEnvAdmin(t, "pw", 12)
	a, _ := st.CreateApp("A", "k1")
	b2, _ := st.CreateApp("B", "k2")
	if resp, _ := portalReq(t, portal, "GET", "/api/tokens", "", nil); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("no session = %d; want 403", resp.StatusCode)
	}
	resp, _ := portalReq(t, portal, "POST", "/api/admin/session", `{"password":"pw"}`, nil)
	admin := map[string]string{"Cookie": adminCookie + "=" + resp.Cookies()[0].Value}

	resp, b := portalReq(t, portal, "POST", "/api/tokens", `{"name":"agent","appIds":["`+a.ID+`","`+b2.ID+`"]}`, admin)
	var created struct {
		ID, Token, Prefix string
		AppIDs            []string
	}
	mustJSON(t, b, &created)
	if resp.StatusCode != http.StatusCreated || !strings.HasPrefix(created.Token, "dtr_") || len(created.AppIDs) != 2 {
		t.Fatalf("create = %d %s", resp.StatusCode, b)
	}
	if _, err := st.TokenByPlain(created.Token); err != nil {
		t.Fatalf("created token not usable: %v", err)
	}

	_, b = portalReq(t, portal, "GET", "/api/tokens", "", admin)
	if s := string(b); strings.Contains(s, created.Token) || strings.Contains(strings.ToLower(s), "hash") || !strings.Contains(s, created.Prefix) {
		t.Fatalf("list must show prefix only: %s", s)
	}

	for _, body := range []string{`{"name":"","appIds":["` + a.ID + `"]}`, `{"name":"x","appIds":[]}`, `{"name":"x"}`, `{"name":"x","appIds":["nope"]}`} {
		want := http.StatusBadRequest
		if strings.Contains(body, "nope") {
			want = http.StatusNotFound
		}
		if resp, b := portalReq(t, portal, "POST", "/api/tokens", body, admin); resp.StatusCode != want {
			t.Errorf("create %s = %d %s; want %d", body, resp.StatusCode, b, want)
		}
	}

	resp, b = portalReq(t, portal, "PATCH", "/api/tokens/"+created.ID, `{"appIds":["`+b2.ID+`"]}`, admin)
	var patched struct {
		Name   string
		AppIDs []string
	}
	mustJSON(t, b, &patched)
	if resp.StatusCode != http.StatusOK || len(patched.AppIDs) != 1 || patched.AppIDs[0] != b2.ID || patched.Name != "agent" {
		t.Fatalf("patch = %d %s", resp.StatusCode, b)
	}
	if resp, _ := portalReq(t, portal, "PATCH", "/api/tokens/"+created.ID, `{"appIds":[]}`, admin); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("patch empty apps = %d; want 400", resp.StatusCode)
	}

	if resp, _ := portalReq(t, portal, "DELETE", "/api/tokens/"+created.ID, "", admin); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete = %d", resp.StatusCode)
	}
	if _, b = portalReq(t, portal, "GET", "/api/tokens", "", admin); strings.TrimSpace(string(b)) != "[]" {
		t.Fatalf("list after delete = %s", b)
	}
}
