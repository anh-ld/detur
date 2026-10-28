package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"detur.dev/server/internal/httpx"
	"detur.dev/server/internal/store"
)

func healthOf(t *testing.T, portal *httptest.Server, appID string) map[string]healthCheck {
	t.Helper()
	resp, b := portalReq(t, portal, "GET", "/api/apps/"+appID+"/health", "", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("health: %d %s", resp.StatusCode, b)
	}
	var list []healthCheck
	mustJSON(t, b, &list)
	out := map[string]healthCheck{}
	for _, c := range list {
		out[c.Check] = c
	}
	return out
}

func TestHealthChecks(t *testing.T) {
	portal, sdk, st := newPortalEnv(t)
	app, _ := setup(t, st)
	if resp, _ := portalReq(t, portal, "GET", "/api/apps/missing/health", "", nil); resp.StatusCode != http.StatusNotFound {
		t.Errorf("unknown app: %d; want 404", resp.StatusCode)
	}

	h := healthOf(t, portal, app.ID)
	for check, want := range map[string]string{"SDK": "fail", "iOS app ID": "warn", "Android": "warn", "Proxy": "ok"} {
		if h[check].Status != want {
			t.Errorf("fresh app %s = %+v; want %s", check, h[check], want)
		}
	}
	if _, ok := h["AASA refresh"]; ok {
		t.Error("AASA refresh shown without several iOS apps")
	}

	// authed SDK call → version + last seen
	if resp, b := portalReq(t, sdk, "POST", "/api/analytics/event", `{"event_name":"x"}`, authHeaders(app.ID)); resp.StatusCode != http.StatusOK {
		t.Fatalf("event: %d %s", resp.StatusCode, b)
	}
	if c := healthOf(t, portal, app.ID)["SDK"]; c.Status != "ok" || !strings.Contains(c.Detail, "react-native/2.3.1") {
		t.Errorf("SDK after call = %+v; want ok with version", c)
	}

	// bad formats fail; 2nd iOS app → fresh link waits for AASA
	if err := st.UpdateAppDetails(app.ID, "com.example.app", "com.example", "AA:BB", ""); err != nil {
		t.Fatal(err)
	}
	other, _ := st.CreateApp("other", "k2")
	if err := st.UpdateAppDetails(other.ID, "ABCDE12345.com.other", "", "", ""); err != nil {
		t.Fatal(err)
	}
	h = healthOf(t, portal, app.ID)
	if h["iOS app ID"].Status != "fail" || h["Android"].Status != "fail" {
		t.Errorf("bad details = %+v / %+v; want fail", h["iOS app ID"], h["Android"])
	}
	if c := h["AASA refresh"]; c.Status != "warn" || !strings.Contains(c.Detail, "/abc") {
		t.Errorf("AASA refresh = %+v; want warn naming /abc", c)
	}

	if err := st.UpdateAppDetails(app.ID, "ABCDE12345.com.example.app", "com.example.app", strings.TrimSuffix(strings.Repeat("AB:", 32), ":"), ""); err != nil {
		t.Fatal(err)
	}
	h = healthOf(t, portal, app.ID)
	if h["iOS app ID"].Status != "ok" || h["Android"].Status != "ok" {
		t.Errorf("good details = %+v / %+v; want ok", h["iOS app ID"], h["Android"])
	}

	// proxy off + private peers → fail
	httpx.TrustProxy = false
	for range minPeers {
		httpx.RemoteIP(&http.Request{RemoteAddr: "10.0.0.2:5000"})
	}
	if c := healthOf(t, portal, app.ID)["Proxy"]; c.Status != "fail" {
		t.Errorf("Proxy = %+v; want fail", c)
	}

	// untrusted peer sending XFF with TrustProxy=true → Forwarded headers warn
	defer func() {
		httpx.TrustProxy = false
		_ = httpx.SetTrustedProxies(nil)
	}()
	httpx.TrustProxy = true
	_ = httpx.SetTrustedProxies(nil)
	httpx.RemoteIP(&http.Request{
		RemoteAddr: "203.0.113.50:5678",
		Header:     http.Header{"X-Forwarded-For": {"198.51.100.1"}},
	})
	if c := healthOf(t, portal, app.ID)["Forwarded headers"]; c.Status != "warn" || !strings.Contains(c.Detail, "203.0.113.50") {
		t.Errorf("Forwarded headers = %+v; want warn naming 203.0.113.50", c)
	}

	// multi-app iOS exceeding MaxAASAKeys on app.ID → AASA keys warn on app, not on other
	for i := range store.MaxAASAKeys {
		key := fmt.Sprintf("cap-%03d", i)
		if _, err := st.CreateLink(store.Link{AppID: app.ID, Key: key, URL: "https://example.com/" + key}); err != nil {
			t.Fatalf("CreateLink %s: %v", key, err)
		}
	}
	if c := healthOf(t, portal, app.ID)["AASA keys"]; c.Status != "warn" || !strings.Contains(c.Detail, "ABCDE12345.com.example.app") {
		t.Errorf("AASA keys on capped app = %+v; want warn naming ABCDE12345.com.example.app", c)
	}
	if _, ok := healthOf(t, portal, other.ID)["AASA keys"]; ok {
		t.Errorf("AASA keys shown on sibling app with 0 links; want omitted")
	}
}
