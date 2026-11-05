package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"detur.dev/server/internal/httpx"
	"detur.dev/server/internal/pipeline"
	"detur.dev/server/internal/store"
)

// newDataEnv: public listener with data API + MCP + short-link pipeline, plus the portal, one store.
func newDataEnv(t *testing.T, adminPassword string) (portal, pub *httptest.Server, st *store.Store) {
	t.Helper()
	portal, _, st = newPortalEnvAdmin(t, adminPassword, 12)
	mux := http.NewServeMux()
	RegisterData(mux, st, adminPassword)
	pipeline.Register(mux, st, 24)
	pub = httptest.NewServer(mux)
	t.Cleanup(pub.Close)
	return portal, pub, st
}

func bearer(tok string) map[string]string { return map[string]string{"Authorization": "Bearer " + tok} }

func TestDataAPI(t *testing.T) {
	portal, pub, st := newDataEnv(t, "pw")
	a, linkA := setup(t, st)
	b, _ := st.CreateApp("other app", "k2")
	linkB, err := st.CreateLink(store.Link{AppID: b.ID, Key: "bkey", URL: "https://example.com/b"})
	if err != nil {
		t.Fatal(err)
	}
	tokA, plainA, err := st.CreateToken("agent", []string{a.ID})
	if err != nil {
		t.Fatal(err)
	}
	h := bearer(plainA)
	get := func(path string, hdr map[string]string) (int, string) {
		resp, b := portalReq(t, pub, "GET", path, "", hdr)
		return resp.StatusCode, string(b)
	}

	t.Run("gate", func(t *testing.T) {
		for name, hdr := range map[string]map[string]string{
			"none": nil, "garbage": bearer("dtr_garbage"), "empty": {"Authorization": "Bearer "},
		} {
			if c, _ := get("/api/v1/apps", hdr); c != http.StatusUnauthorized {
				t.Errorf("%s bearer = %d; want 401", name, c)
			}
		}
		if c, _ := get("/api/v1/apps", map[string]string{"Authorization": h["Authorization"], "Origin": "https://evil"}); c != http.StatusForbidden {
			t.Errorf("Origin = %d; want 403", c)
		}
	})

	t.Run("AE1 other app indistinguishable from unknown", func(t *testing.T) {
		c1, b1 := get("/api/v1/apps/"+b.ID+"/analytics", h)
		c2, b2 := get("/api/v1/apps/nope/analytics", h)
		if c1 != 404 || c2 != 404 || b1 != b2 {
			t.Fatalf("B = %d %q, unknown = %d %q; want identical 404", c1, b1, c2, b2)
		}
		for _, p := range []string{"/links", "/match-quality", "/fraud", "/health", ""} {
			if c, _ := get("/api/v1/apps/"+b.ID+p, h); c != 404 {
				t.Errorf("B%s = %d; want 404", p, c)
			}
		}
		// B's link under A's path: 404 for rules and variants
		for _, p := range []string{"/rules", "/variants"} {
			if c, _ := get("/api/v1/apps/"+a.ID+"/links/"+linkB.ID+p, h); c != 404 {
				t.Errorf("B link %s under A = %d; want 404", p, c)
			}
			if c, _ := get("/api/v1/apps/"+a.ID+"/links/"+linkA.ID+p, h); c != 200 {
				t.Errorf("A link %s = %d; want 200", p, c)
			}
		}
	})

	t.Run("AE2 new app not visible", func(t *testing.T) {
		if _, err := st.CreateApp("C", "k3"); err != nil {
			t.Fatal(err)
		}
		_, body := get("/api/v1/apps", h)
		var apps []map[string]any
		mustJSON(t, []byte(body), &apps)
		if len(apps) != 1 || apps[0]["id"] != a.ID {
			t.Fatalf("apps = %s; want only A", body)
		}
	})

	t.Run("AE5 no writes", func(t *testing.T) {
		resp, _ := portalReq(t, pub, "POST", "/api/v1/apps/"+a.ID+"/links", `{"key":"zzz","url":"https://example.com"}`, h)
		if resp.StatusCode != http.StatusMethodNotAllowed {
			t.Errorf("POST links = %d; want 405", resp.StatusCode)
		}
		if ls, _ := st.ListLinks(a.ID); len(ls) != 1 {
			t.Errorf("links = %d; want 1", len(ls))
		}
	})

	t.Run("R6 safe shapes", func(t *testing.T) {
		_, body := get("/api/v1/apps/"+a.ID, h)
		if strings.Contains(strings.ToLower(body), "hash") || strings.Contains(body, "page") || !strings.Contains(body, a.ID) {
			t.Errorf("app = %s", body)
		}
		st.RecordInstall(store.Install{AppID: a.ID, DeviceHash: "d", Attribution: store.AttributionOrganic, Method: store.MethodOrganic, Fraud: "timing"})
		_, body = get("/api/v1/apps/"+a.ID+"/fraud", h)
		if !strings.Contains(body, `"signals"`) || strings.Contains(body, "installs") {
			t.Errorf("fraud = %s", body)
		}
	})

	t.Run("health drops server-wide checks", func(t *testing.T) {
		httpx.TrustProxy = true
		t.Cleanup(func() { httpx.TrustProxy = false; _ = httpx.SetTrustedProxies(nil) })
		_ = httpx.SetTrustedProxies(nil)
		httpx.RemoteIP(&http.Request{RemoteAddr: "203.0.113.50:5678", Header: http.Header{"X-Forwarded-For": {"198.51.100.1"}}})
		if c, ok := healthOf(t, portal, a.ID)["Forwarded headers"]; !ok {
			t.Fatalf("precondition: portal health lacks Forwarded headers: %+v", c)
		}
		_, body := get("/api/v1/apps/"+a.ID+"/health", h)
		if strings.Contains(body, "Proxy") || strings.Contains(body, "Forwarded") || strings.Contains(body, "203.0.113.50") || !strings.Contains(body, `"SDK"`) {
			t.Errorf("health = %s", body)
		}
	})

	t.Run("analytics equals portal", func(t *testing.T) {
		recordAndroidClick(t, st, a, linkA)
		now := time.Now().UTC()
		rng := "?from=" + now.AddDate(0, 0, -6).Format("2006-01-02") + "&to=" + now.Format("2006-01-02")
		for _, q := range []string{"?days=30", rng, "?days=7&platform=android"} {
			_, want := portalReq2(t, portal, "/api/apps/"+a.ID+"/analytics"+q)
			_, got := get("/api/v1/apps/"+a.ID+"/analytics"+q, h)
			if got != want || !strings.Contains(got, `"clicks":1`) {
				t.Errorf("analytics%s differs:\n data   %s\n portal %s", q, got, want)
			}
		}
	})

	t.Run("AE3 revoke", func(t *testing.T) {
		if c, _ := get("/api/v1/apps", h); c != 200 {
			t.Fatalf("before revoke = %d", c)
		}
		if err := st.DeleteToken(tokA.ID); err != nil {
			t.Fatal(err)
		}
		if c, _ := get("/api/v1/apps", h); c != http.StatusUnauthorized {
			t.Errorf("after revoke = %d; want 401", c)
		}
	})
}

func portalReq2(t *testing.T, ts *httptest.Server, path string) (int, string) {
	resp, b := portalReq(t, ts, "GET", path, "", nil)
	return resp.StatusCode, string(b)
}

// KTD5: ADMIN_PASSWORD unset -> data API and MCP refuse every token, even valid rows.
func TestDataAPIFailsClosedWithoutAdminPassword(t *testing.T) {
	_, pub, st := newDataEnv(t, "")
	a, _ := st.CreateApp("A", "k1")
	_, plain, _ := st.CreateToken("t", []string{a.ID})
	if resp, _ := portalReq(t, pub, "GET", "/api/v1/apps", "", bearer(plain)); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("GET apps = %d; want 401", resp.StatusCode)
	}
	if resp, _ := portalReq(t, pub, "POST", "/api/mcp", `{"jsonrpc":"2.0","id":1,"method":"ping"}`, bearer(plain)); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("MCP = %d; want 401", resp.StatusCode)
	}
}
