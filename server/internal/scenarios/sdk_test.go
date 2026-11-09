package scenarios

import (
	"database/sql"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"detur.dev/server/internal/store"
)

const (
	sdkOtherIP   = "198.51.100.23"
	sdkTapScreen = "_dt=1&screen=393x852%403&tz=Asia%2FHo_Chi_Minh"
	sdkLang      = "vi-VN,vi;q=0.9,en-US;q=0.8"
)

func sdkRoute(a store.App, r string) string { return "/" + a.ID[:3] + r }

func sdkLink(t *testing.T, r resp) string {
	t.Helper()
	must(t, r.Code == http.StatusOK, "status = %d body %q, want 200", r.Code, r.Body)
	var b struct {
		Link string `json:"link"`
	}
	r.JSON(t, &b)
	return b.Link
}

func (e *env) sdkTapH(path, ua string, hdr map[string]string) resp {
	e.t.Helper()
	if r := e.tapH(path, ua, hdr); r.Code != http.StatusOK {
		e.t.Fatalf("hop 1 %s = %d, want 200", path, r.Code)
	}
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	return e.tapH(path+sep+sdkTapScreen, ua, hdr)
}

func sdkClickID(t *testing.T, r resp) string {
	t.Helper()
	must(t, r.Code == http.StatusFound, "android hop 2 = %d, want 302 to Play", r.Code)
	u, err := url.Parse(r.Loc())
	must(t, err == nil, "bad Location %q", r.Loc())
	ref, _ := url.ParseQuery(u.Query().Get("referrer"))
	id := ref.Get("click_id")
	must(t, id != "", "no click_id in Play referrer: %q", r.Loc())
	return id
}

var sdkReDC = regexp.MustCompile(`_dc=([A-Za-z0-9_-]+)`)

func (e *env) sdkPasteTap(key, ua string, hdr map[string]string) string {
	e.t.Helper()
	h1 := e.tapH("/"+key, ua, hdr)
	m := sdkReDC.FindStringSubmatch(h1.Body)
	if h1.Code != http.StatusOK || m == nil {
		e.t.Fatalf("copy page = %d, no _dc token in body", h1.Code)
	}
	pasted := e.Pub.URL + "/" + key + "?_dc=" + m[1]
	e.tapH("/"+key+"?"+sdkTapScreen+"&pasted_link="+url.QueryEscape(pasted), ua, hdr)
	return pasted
}

func (e *env) sdkAge(a store.App, d time.Duration) {
	e.t.Helper()
	db, err := sql.Open("sqlite", e.DBPath)
	if err != nil {
		e.t.Fatal(err)
	}
	defer db.Close()
	rows, err := db.Query(`SELECT id, created_at FROM clicks WHERE app_id = ?`, a.ID)
	if err != nil {
		e.t.Fatal(err)
	}
	type row struct{ id, at string }
	var all []row
	for rows.Next() {
		var r row
		_ = rows.Scan(&r.id, &r.at)
		all = append(all, r)
	}
	rows.Close()
	must(e.t, len(all) > 0, "no clicks to age")
	for _, r := range all {
		at, err := time.Parse("2006-01-02T15:04:05.000Z", r.at)
		if err != nil {
			e.t.Fatalf("parse created_at %q: %v", r.at, err)
		}
		old := at.Add(-d).UTC().Format("2006-01-02T15:04:05.000Z")
		if _, err := db.Exec(`UPDATE clicks SET created_at = ?, first_seen_at = ? WHERE id = ?`, old, old, r.id); err != nil {
			e.t.Fatal(err)
		}
	}
}

func (e *env) sdkFrom(a store.App, ip, path, body string) resp {
	e.t.Helper()
	return e.sdkH(a, path, body, map[string]string{"X-Forwarded-For": ip})
}

func sdkEventName(t *testing.T, e *env, a store.App) []string {
	t.Helper()
	db, err := sql.Open("sqlite", e.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	rows, err := db.Query(`SELECT event FROM events WHERE app_id = ? ORDER BY rowid`, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		_ = rows.Scan(&s)
		out = append(out, s)
	}
	return out
}

func TestS_SDK_01_AndroidReferrerClickIDMatches(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.link(a, "promo", "/product/42")
	id := sdkClickID(t, e.mobileTap("/promo", uaAndroidChrome))
	got := sdkLink(t, e.sdk(a, "/api/link/match-link", `{"clickId":"`+id+`"}`))
	must(t, got == sdkRoute(a, "/product/42"), "link = %q, want %q", got, sdkRoute(a, "/product/42"))
}

func TestS_SDK_02_AndroidReferrerForwardsTappedQuery(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.link(a, "promo", "/product/42")
	id := sdkClickID(t, e.mobileTap("/promo?coupon=SAVE10", uaAndroidChrome))
	got := sdkLink(t, e.sdk(a, "/api/link/match-link", `{"clickId":"`+id+`"}`))
	must(t, strings.HasPrefix(got, sdkRoute(a, "/product/42?")) && strings.Contains(got, "coupon=SAVE10"), "link = %q, want route with coupon=SAVE10", got)
	must(t, !strings.Contains(got, "_dt") && !strings.Contains(got, "screen=") && !strings.Contains(got, "tz="), "internal params leaked: %q", got)
}

func TestS_SDK_03_AndroidReferrerDaysLaterStillMatches(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.link(a, "promo", "/product/42")
	id := sdkClickID(t, e.mobileTap("/promo", uaAndroidChrome))
	e.sdkAge(a, 72*time.Hour)
	got := sdkLink(t, e.sdk(a, "/api/link/match-link", `{"clickId":"`+id+`"}`))
	must(t, got == sdkRoute(a, "/product/42"), "link = %q", got)
}

func TestS_SDK_04_UnknownClickIDIsOrganic(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.link(a, "promo", "/product/42")
	e.mobileTap("/promo", uaAndroidChrome)
	r := e.sdk(a, "/api/link/match-link", `{"clickId":"notarealclickid1"}`)
	must(t, r.Code == http.StatusNotFound, "status = %d body %q, want 404", r.Code, r.Body)
}

func TestS_SDK_05_ClickIDOfOtherAppNeverMatches(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	b, err := e.St.CreateApp("Other App", "dk_other-key-9876543210")
	must(t, err == nil, "CreateApp: %v", err)
	e.link(b, "bpromo", "/b/secret")
	id := sdkClickID(t, e.mobileTap("/bpromo", uaAndroidChrome))
	r := e.sdk(a, "/api/link/match-link", `{"clickId":"`+id+`"}`)
	must(t, r.Code == http.StatusNotFound, "status = %d body %q, want 404", r.Code, r.Body)
}

func TestS_SDK_06_ClickIDRetryIsIdempotent(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.link(a, "promo", "/product/42")
	id := sdkClickID(t, e.mobileTap("/promo", uaAndroidChrome))
	body := `{"clickId":"` + id + `"}`
	first := sdkLink(t, e.sdk(a, "/api/link/match-link", body))
	second := sdkLink(t, e.sdk(a, "/api/link/match-link", body))
	must(t, first == second && first == sdkRoute(a, "/product/42"), "first %q second %q", first, second)
}

func TestS_SDK_07_ClickIDConsumedNotReusedBySibling(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.link(a, "promo", "/product/42")
	id := sdkClickID(t, e.mobileTap("/promo", uaAndroidChrome))
	sdkLink(t, e.sdk(a, "/api/link/match-link", `{"clickId":"`+id+`"}`))
	r := e.sdk(a, "/api/link/match-link", androidFingerprint(""))
	must(t, r.Code == http.StatusNotFound, "sibling status = %d body %q, want 404", r.Code, r.Body)
}

func TestS_SDK_08_IOSFingerprintMatches(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.link(a, "promo", "/product/42")
	e.mobileTap("/promo", uaIOSSafari)
	got := sdkLink(t, e.sdk(a, "/api/link/match-link", iosFingerprint("")))
	must(t, got == sdkRoute(a, "/product/42"), "link = %q", got)
}

func TestS_SDK_09_AndroidNoReferrerFingerprintMatches(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.link(a, "promo", "/product/42")
	e.mobileTap("/promo", uaAndroidChrome)
	got := sdkLink(t, e.sdk(a, "/api/link/match-link", androidFingerprint("")))
	must(t, got == sdkRoute(a, "/product/42"), "link = %q", got)
}

func TestS_SDK_10_NoClickIsOrganic(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.link(a, "promo", "/product/42")
	r := e.sdk(a, "/api/link/match-link", iosFingerprint(""))
	must(t, r.Code == http.StatusNotFound, "status = %d body %q, want 404", r.Code, r.Body)
}

func TestS_SDK_11_IOSAfterWindowIsOrganic(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.link(a, "promo", "/product/42")
	e.mobileTap("/promo", uaIOSSafari)
	e.sdkAge(a, 25*time.Minute)
	r := e.sdk(a, "/api/link/match-link", iosFingerprint(""))
	must(t, r.Code == http.StatusNotFound, "status = %d body %q, want 404", r.Code, r.Body)
}

func TestS_SDK_12_CustomWindowMatchesSlowInstall(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	must(t, e.St.UpdateAppMatchSettings(a.ID, 850, 60) == nil, "settings")
	e.link(a, "promo", "/product/42")
	e.mobileTap("/promo", uaIOSSafari)
	e.sdkAge(a, 40*time.Minute)
	got := sdkLink(t, e.sdk(a, "/api/link/match-link", iosFingerprint("")))
	must(t, got == sdkRoute(a, "/product/42"), "link = %q", got)
}

func TestS_SDK_13_SameWiFiNonClickerStaysOrganic(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.link(a, "promo", "/product/42")
	e.mobileTap("/promo", uaIOSSafari)
	peer := strings.NewReplacer(`"iPhone15,2"`, `"iPhone16,2"`, `"17.5"`, `"17.4"`, `"screenWidth":393`, `"screenWidth":430`, `"screenHeight":852`, `"screenHeight":932`).Replace(iosFingerprint(""))
	r := e.sdk(a, "/api/link/match-link", peer)
	must(t, r.Code == http.StatusNotFound, "peer status = %d body %q, want 404", r.Code, r.Body)
	got := sdkLink(t, e.sdk(a, "/api/link/match-link", iosFingerprint("")))
	must(t, got == sdkRoute(a, "/product/42"), "clicker link = %q", got)
}

func TestS_SDK_14_SameWiFiOtherAndroidModelOrganic(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.link(a, "promo", "/product/42")
	e.mobileTap("/promo", uaAndroidChrome)
	peer := strings.NewReplacer(`"Pixel 7"`, `"SM-S911B"`, `"Google"`, `"samsung"`, `"screenWidth":393`, `"screenWidth":360`, `"screenHeight":852`, `"screenHeight":780`).Replace(androidFingerprint(""))
	r := e.sdk(a, "/api/link/match-link", peer)
	must(t, r.Code == http.StatusNotFound, "peer status = %d body %q, want 404", r.Code, r.Body)
}

func TestS_SDK_15_IOSWiFiToCellularMatches(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.link(a, "promo", "/product/42")
	e.sdkTapH("/promo", uaIOSSafari, map[string]string{"Accept-Language": sdkLang})
	got := sdkLink(t, e.sdkFrom(a, sdkOtherIP, "/api/link/match-link", iosFingerprint("")))
	must(t, got == sdkRoute(a, "/product/42"), "link = %q", got)
}

func TestS_SDK_16_AndroidVPNChangeMatches(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.link(a, "promo", "/product/42")
	e.sdkTapH("/promo", uaAndroidChrome, map[string]string{"Accept-Language": sdkLang})
	got := sdkLink(t, e.sdkFrom(a, sdkOtherIP, "/api/link/match-link", androidFingerprint("")))
	must(t, got == sdkRoute(a, "/product/42"), "link = %q", got)
}

func TestS_SDK_17_TimezoneMismatchSameIPMatches(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.link(a, "promo", "/product/42")
	e.tap("/promo", uaIOSSafari)
	e.tap("/promo?_dt=1&screen=393x852%403&tz=UTC", uaIOSSafari)
	got := sdkLink(t, e.sdk(a, "/api/link/match-link", iosFingerprint("")))
	must(t, got == sdkRoute(a, "/product/42"), "link = %q", got)
}

func TestS_SDK_18_LandscapeFirstLaunchMatches(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.link(a, "promo", "/product/42")
	e.mobileTap("/promo", uaIOSSafari)
	body := strings.NewReplacer(`"screenWidth":393,"screenHeight":852`, `"screenWidth":852,"screenHeight":393`).Replace(iosFingerprint(""))
	got := sdkLink(t, e.sdk(a, "/api/link/match-link", body))
	must(t, got == sdkRoute(a, "/product/42"), "link = %q", got)
}

func TestS_SDK_19_NullTimezoneStillMatches(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.link(a, "promo", "/product/42")
	e.mobileTap("/promo", uaIOSSafari)
	body := strings.Replace(iosFingerprint(""), `"timezone":"Asia/Ho_Chi_Minh"`, `"timezone":null`, 1)
	got := sdkLink(t, e.sdk(a, "/api/link/match-link", body))
	must(t, got == sdkRoute(a, "/product/42"), "link = %q", got)
}

func TestS_SDK_20_EmptyLocaleListStillMatches(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.link(a, "promo", "/product/42")
	e.mobileTap("/promo", uaAndroidChrome)
	body := strings.Replace(androidFingerprint(""), `"locale":[{"languageTag":"vi-VN"}]`, `"locale":[]`, 1)
	got := sdkLink(t, e.sdk(a, "/api/link/match-link", body))
	must(t, got == sdkRoute(a, "/product/42"), "link = %q", got)
}

func TestS_SDK_21_UnknownWebViewUAStillMatches(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.link(a, "promo", "/product/42")
	e.mobileTap("/promo", uaIOSSafari)
	body := strings.Replace(iosFingerprint(""), uaIOSAppWebView, "unknown", 1)
	got := sdkLink(t, e.sdk(a, "/api/link/match-link", body))
	must(t, got == sdkRoute(a, "/product/42"), "link = %q", got)
}

func sdkSamsungBody() string {
	return strings.NewReplacer(`"Pixel 7"`, `"SM-S918B"`, `"Google"`, `"samsung"`,
		uaAndroidAppWebView, "Mozilla/5.0 (Linux; Android 14; SM-S918B Build/UP1A.231005.007; wv) AppleWebKit/537.36 (KHTML, like Gecko) Version/4.0 Chrome/126.0.6478.71 Mobile Safari/537.36").Replace(androidFingerprint(""))
}

func TestS_SDK_22_AndroidWebViewUADiffersFromBrowser(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.link(a, "promo", "/product/42")
	e.sdkTapH("/promo", uaSamsung, map[string]string{"Sec-CH-UA-Model": `"SM-S918B"`, "Sec-CH-UA-Platform-Version": `"14.0.0"`})
	got := sdkLink(t, e.sdk(a, "/api/link/match-link", sdkSamsungBody()))
	must(t, got == sdkRoute(a, "/product/42"), "link = %q", got)
}

func TestS_SDK_51_SamsungUAVendorPrefixMatches(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.link(a, "promo", "/product/42")
	e.mobileTap("/promo", uaSamsung)
	got := sdkLink(t, e.sdk(a, "/api/link/match-link", sdkSamsungBody()))
	must(t, got == sdkRoute(a, "/product/42"), "link = %q", got)
}

func TestS_SDK_23_OtherAppsClickNeverMatches(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	b, err := e.St.CreateApp("Other App", "dk_other-key-9876543210")
	must(t, err == nil, "CreateApp: %v", err)
	e.linkWith(store.Link{AppID: b.ID, Key: "bpromo", URL: "/b/offer", IOS: "https://apps.apple.com/app/id999", FallbackURL: "https://b.example"})
	e.mobileTap("/bpromo", uaIOSSafari)
	r := e.sdk(a, "/api/link/match-link", iosFingerprint(""))
	must(t, r.Code == http.StatusNotFound, "app A status = %d body %q, want 404", r.Code, r.Body)
	rb := e.sdkH(b, "/api/link/match-link", iosFingerprint(""), map[string]string{"Authorization": "Bearer dk_other-key-9876543210"})
	got := sdkLink(t, rb)
	must(t, got == sdkRoute(b, "/b/offer"), "app B link = %q", got)
}

func TestS_SDK_24_NewestOfTwoTapsWins(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.link(a, "first", "/product/1")
	e.link(a, "second", "/product/2")
	e.mobileTap("/first", uaIOSSafari)
	e.sdkAge(a, time.Minute)
	e.mobileTap("/second", uaIOSSafari)
	got := sdkLink(t, e.sdk(a, "/api/link/match-link", iosFingerprint("")))
	must(t, got == sdkRoute(a, "/product/2"), "link = %q, want newest %q", got, sdkRoute(a, "/product/2"))
}

func TestS_SDK_25_DesktopClickDoesNotMatchPhone(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.link(a, "promo", "/product/42")
	e.tap("/promo", uaDesktopChrome)
	r := e.sdk(a, "/api/link/match-link", iosFingerprint(""))
	must(t, r.Code == http.StatusNotFound, "status = %d body %q, want 404", r.Code, r.Body)
}

func TestS_SDK_26_LinkPreviewFetchDoesNotMatch(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.link(a, "promo", "/product/42")
	e.tap("/promo", uaWhatsApp)
	e.tap("/promo", uaIMessage)
	r := e.sdk(a, "/api/link/match-link", iosFingerprint(""))
	must(t, r.Code == http.StatusNotFound, "status = %d body %q, want 404", r.Code, r.Body)
}

func TestS_SDK_27_InstalledOpenDoesNotFeedDeferredMatch(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.link(a, "promo", "/product/42")
	e.sdkH(a, "/api/link/universal-link-click", `{"url":"https://lnk.example/promo","timestamp":`+nowMS()+`,"platform":"ios"}`,
		map[string]string{"User-Agent": "ScenarioApp/1.0 CFNetwork/1494.0.7 Darwin/23.4.0"})
	r := e.sdk(a, "/api/link/match-link", iosFingerprint(""))
	must(t, r.Code == http.StatusNotFound, "status = %d body %q, want 404", r.Code, r.Body)
}

func TestS_SDK_28_PasteboardTokenMatchesOnCellular(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.link(a, "promo", "/product/42")
	pasted := e.sdkPasteTap("promo", uaIOSSafari, nil)
	got := sdkLink(t, e.sdkFrom(a, sdkOtherIP, "/api/link/match-link", iosFingerprint(`,"pastedLink":"`+pasted+`"`)))
	must(t, got == sdkRoute(a, "/product/42"), "link = %q", got)
}

func TestS_SDK_29_PasteboardTokenMatchesNextDay(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.link(a, "promo", "/product/42")
	pasted := e.sdkPasteTap("promo", uaIOSSafari, nil)
	e.sdkAge(a, 14*time.Hour)
	got := sdkLink(t, e.sdk(a, "/api/link/match-link", iosFingerprint(`,"pastedLink":"`+pasted+`"`)))
	must(t, got == sdkRoute(a, "/product/42"), "link = %q", got)
}

func TestS_SDK_30_PasteboardTokenOnlyOnce(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.link(a, "promo", "/product/42")
	pasted := e.sdkPasteTap("promo", uaIOSSafari, nil)
	sdkLink(t, e.sdk(a, "/api/link/match-link", iosFingerprint(`,"pastedLink":"`+pasted+`"`)))
	ipad := strings.NewReplacer(`"iPhone15,2"`, `"iPad13,18"`, `"screenWidth":393`, `"screenWidth":820`, `"screenHeight":852`, `"screenHeight":1180`, `"scale":3`, `"scale":2`).Replace(iosFingerprint(`,"pastedLink":"` + pasted + `"`))
	r := e.sdk(a, "/api/link/match-link", ipad)
	must(t, r.Code == http.StatusNotFound, "iPad status = %d body %q, want 404", r.Code, r.Body)
}

func TestS_SDK_31_PastedLinkOfOtherAppIgnored(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	b, err := e.St.CreateApp("Other App", "dk_other-key-9876543210")
	must(t, err == nil, "CreateApp: %v", err)
	e.linkWith(store.Link{AppID: b.ID, Key: "bpromo", URL: "/b/offer", IOS: "https://apps.apple.com/app/id999"})
	pasted := e.sdkPasteTap("bpromo", uaIOSSafari, nil)
	r := e.sdkFrom(a, sdkOtherIP, "/api/link/match-link", iosFingerprint(`,"pastedLink":"`+pasted+`"`))
	must(t, r.Code == http.StatusNotFound, "status = %d body %q, want 404", r.Code, r.Body)
}

func TestS_SDK_32_TagLinksAddsKey(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	must(t, e.St.UpdateAppTagLinks(a.ID, true) == nil, "tag links")
	e.link(a, "promo", "/product/42?ref=summer")
	e.mobileTap("/promo", uaIOSSafari)
	got := sdkLink(t, e.sdk(a, "/api/link/match-link", iosFingerprint("")))
	u, err := url.Parse(got)
	must(t, err == nil && u.Path == sdkRoute(a, "/product/42"), "link = %q", got)
	must(t, u.Query().Get("ref") == "summer" && u.Query().Get("detur_link") == "promo", "query = %q", u.RawQuery)
}

func TestS_SDK_33_UniversalLinkClickRecorded(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.link(a, "promo", "/product/42")
	body := `{"url":"https://lnk.example/promo?coupon=SAVE10","timestamp":` + nowMS() + `,"platform":"ios","params":{"coupon":"SAVE10"},"metadata":{"os_version":"17.5","app_version":"1.4.0","device_model":"iPhone 15"}}`
	r := e.sdk(a, "/api/link/universal-link-click", body)
	must(t, r.Code == http.StatusOK, "status = %d body %q", r.Code, r.Body)
	var b struct {
		Allowed bool   `json:"allowed"`
		ClickID string `json:"clickId"`
	}
	r.JSON(t, &b)
	must(t, b.Allowed && b.ClickID != "", "body = %q", r.Body)
	n, _ := e.St.CountClicks(a.ID)
	must(t, n == 1, "clicks = %d, want 1", n)
}

func TestS_SDK_34_UniversalLinkUnknownKeyAllowed(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	r := e.sdk(a, "/api/link/universal-link-click", `{"url":"https://lnk.example/gone","timestamp":`+nowMS()+`,"platform":"android"}`)
	var b struct {
		Allowed bool   `json:"allowed"`
		ClickID string `json:"clickId"`
	}
	r.JSON(t, &b)
	must(t, r.Code == http.StatusOK && b.Allowed && b.ClickID != "", "status %d body %q", r.Code, r.Body)
	n, _ := e.St.CountClicks(a.ID)
	must(t, n == 0, "clicks = %d, want 0", n)
}

func TestS_SDK_35_ResolveShortForwardsQuery(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.link(a, "promo", "/product/42")
	got := sdkLink(t, e.sdk(a, "/api/link/resolve-short", `{"url":"https://lnk.example/promo?coupon=SAVE10"}`))
	must(t, got == sdkRoute(a, "/product/42?coupon=SAVE10"), "link = %q", got)
}

func TestS_SDK_36_ResolveShortDropsPasteToken(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.link(a, "promo", "/product/42")
	got := sdkLink(t, e.sdk(a, "/api/link/resolve-short", `{"url":"https://lnk.example/promo?_dc=AbCdEfGhIjKlMnOp"}`))
	must(t, got == sdkRoute(a, "/product/42"), "link = %q", got)
}

func TestS_SDK_37_ResolveShortUnknownKey404(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.link(a, "promo", "/product/42")
	r := e.sdk(a, "/api/link/resolve-short", `{"url":"https://lnk.example/about"}`)
	must(t, r.Code == http.StatusNotFound, "status = %d body %q, want 404", r.Code, r.Body)
}

func TestS_SDK_38_ResolveShortExpired404(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	past := time.Now().Add(-time.Hour)
	e.linkWith(store.Link{AppID: a.ID, Key: "xmas", URL: "/sale", IOS: "https://apps.apple.com/app/id1234567890", ExpiresAt: &past})
	r := e.sdk(a, "/api/link/resolve-short", `{"url":"https://lnk.example/xmas"}`)
	must(t, r.Code == http.StatusNotFound, "status = %d body %q, want 404", r.Code, r.Body)
}

func TestS_SDK_39_ResolveShortOtherAppsKey404(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	b, err := e.St.CreateApp("Other App", "dk_other-key-9876543210")
	must(t, err == nil, "CreateApp: %v", err)
	e.linkWith(store.Link{AppID: b.ID, Key: "bpromo", URL: "/b/secret"})
	r := e.sdk(a, "/api/link/resolve-short", `{"url":"https://lnk.example/bpromo"}`)
	must(t, r.Code == http.StatusNotFound, "status = %d body %q, want 404", r.Code, r.Body)
}

func TestS_SDK_40_RetentionAppOpenStored(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	r := e.sdk(a, "/api/analytics/retention", `{"event_name":"app_open","timestamp":"2026-10-28T08:00:00.000Z","platform":"ios","device_id":"dev-1"}`)
	must(t, r.Code == http.StatusOK && strings.Contains(r.Body, `"success":true`), "status %d body %q", r.Code, r.Body)
	names := sdkEventName(t, e, a)
	must(t, len(names) == 1 && names[0] == "app_open", "events = %v", names)
}

func TestS_SDK_41_OpenedViaUniversalLinkEvent(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.link(a, "promo", "/product/42")
	r := e.sdk(a, "/api/analytics/event", `{"event_name":"opened_via_universal_link","data":{"link":"https://lnk.example/promo"},"timestamp":"2026-10-28T08:00:00.000Z","platform":"android","device_id":"dev-2"}`)
	must(t, r.Code == http.StatusOK, "status %d body %q", r.Code, r.Body)
	names := sdkEventName(t, e, a)
	must(t, len(names) == 1 && names[0] == "opened_via_universal_link", "events = %v", names)
}

func TestS_SDK_42_CustomEventsWithAndWithoutData(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	r1 := e.sdk(a, "/api/analytics/event", `{"event_name":"purchase","data":{"amount":9.99,"currency":"USD","items":[{"sku":"A1","qty":2}]},"timestamp":"2026-10-28T08:00:00.000Z","platform":"ios","device_id":"dev-3"}`)
	r2 := e.sdk(a, "/api/analytics/event", `{"event_name":"tutorial_done","timestamp":"2026-10-28T08:00:01.000Z","platform":"ios","device_id":"dev-3"}`)
	must(t, r1.Code == http.StatusOK && r2.Code == http.StatusOK, "statuses %d %d", r1.Code, r2.Code)
	names := sdkEventName(t, e, a)
	must(t, len(names) == 2 && names[0] == "purchase" && names[1] == "tutorial_done", "events = %v", names)
}

func TestS_SDK_43_BadCredentialsRejected(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	b, err := e.St.CreateApp("Other App", "dk_other-key-9876543210")
	must(t, err == nil, "CreateApp: %v", err)
	cases := map[string]map[string]string{
		"no auth":      {"Authorization": ""},
		"empty bearer": {"Authorization": "Bearer "},
		"wrong key":    {"Authorization": "Bearer dk_wrong-key-000000000"},
		"basic scheme": {"Authorization": "Basic " + apiKey},
		"other app id": {"X-App-ID": b.ID},
		"no app id":    {"X-App-ID": ""},
		"no sdk":       {"X-SDK": ""},
	}
	for name, h := range cases {
		r := e.sdkH(a, "/api/analytics/event", `{"event_name":"x","device_id":"d"}`, h)
		must(t, r.Code == http.StatusUnauthorized, "%s: status = %d, want 401", name, r.Code)
	}
	n, _ := e.St.CountEvents(a.ID)
	must(t, n == 0, "events written = %d", n)
}

func TestS_SDK_44_RotatedKeyOldRejected(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.link(a, "promo", "/product/42")
	must(t, e.St.UpdateAppKey(a.ID, "dk_new-key-abcdef0123456") == nil, "rotate")
	old := e.sdk(a, "/api/link/resolve-short", `{"url":"https://lnk.example/promo"}`)
	must(t, old.Code == http.StatusUnauthorized, "old key status = %d", old.Code)
	nw := e.sdkH(a, "/api/link/resolve-short", `{"url":"https://lnk.example/promo"}`, map[string]string{"Authorization": "Bearer dk_new-key-abcdef0123456"})
	must(t, nw.Code == http.StatusOK, "new key status = %d body %q", nw.Code, nw.Body)
}

func TestS_SDK_45_RevokedKeyRejected(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	must(t, e.St.ClearAppKey(a.ID) == nil, "revoke")
	r := e.sdk(a, "/api/link/match-link", iosFingerprint(""))
	must(t, r.Code == http.StatusUnauthorized, "status = %d body %q, want 401", r.Code, r.Body)
}

func TestS_SDK_46_MalformedBodiesAre4xx(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.link(a, "promo", "/product/42")
	e.mobileTap("/promo", uaIOSSafari)
	for _, body := range []string{`{"model":`, ``, `[]`, `"hello"`} {
		for _, p := range []string{"/api/link/match-link", "/api/link/resolve-short", "/api/analytics/event"} {
			r := e.sdk(a, p, body)
			must(t, r.Code >= 400 && r.Code < 500, "%s body %q: status = %d, want 4xx", p, body, r.Code)
		}
	}
}

func TestS_SDK_47_HugeBodyRejectedServerSurvives(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.link(a, "promo", "/product/42")
	huge := `{"event_name":"x","data":{"blob":"` + strings.Repeat("A", 2<<20) + `"}}`
	r := e.sdk(a, "/api/analytics/event", huge)
	must(t, r.Code >= 400 && r.Code < 500, "huge status = %d, want 4xx", r.Code)
	n, _ := e.St.CountEvents(a.ID)
	must(t, n == 0, "huge event stored")
	got := sdkLink(t, e.sdk(a, "/api/link/resolve-short", `{"url":"https://lnk.example/promo"}`))
	must(t, got == sdkRoute(a, "/product/42"), "after huge: link = %q", got)
}

func TestS_SDK_48_LenientContentType(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.link(a, "promo", "/product/42")
	for _, ct := range []string{"text/plain;charset=UTF-8", "application/json; charset=utf-8"} {
		r := e.sdkH(a, "/api/link/resolve-short", `{"url":"https://lnk.example/promo"}`, map[string]string{"Content-Type": ct})
		must(t, sdkLink(t, r) == sdkRoute(a, "/product/42"), "%s: body %q", ct, r.Body)
	}
}

func TestS_SDK_49_UnknownFieldsIgnored(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.link(a, "promo", "/product/42")
	e.mobileTap("/promo", uaIOSSafari)
	body := iosFingerprint(`,"deviceId":"abc","appVersion":"2.0.0","extra":{"isEmulator":false,"carriers":["Viettel"]}`)
	got := sdkLink(t, e.sdk(a, "/api/link/match-link", body))
	must(t, got == sdkRoute(a, "/product/42"), "link = %q", got)
}

func TestS_SDK_50_FingerprintRetryIdempotent(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.link(a, "promo", "/product/42")
	e.mobileTap("/promo", uaIOSSafari)
	body := iosFingerprint("")
	first := sdkLink(t, e.sdk(a, "/api/link/match-link", body))
	second := sdkLink(t, e.sdk(a, "/api/link/match-link", body))
	must(t, first == second, "first %q second %q", first, second)
	org, non, _ := e.St.CountInstalls(a.ID)
	must(t, non == 1 && org == 0, "installs organic=%d non=%d, want 0/1", org, non)
}

func TestS_SDK_52_UnrelatedClipboardStillMatches(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.link(a, "promo", "/product/42")
	e.mobileTap("/promo", uaIOSSafari)
	got := sdkLink(t, e.sdk(a, "/api/link/match-link", iosFingerprint(`,"pastedLink":"https://news.example.com/2026/10/story?_dc=AbCdEfGhIjKlMnOp"`)))
	must(t, got == sdkRoute(a, "/product/42"), "link = %q", got)
}

func TestS_SDK_53_WrongDeviceClockStillMatches(t *testing.T) {
	for name, off := range map[string]time.Duration{"ahead": 5 * time.Hour, "behind": -72 * time.Hour} {
		e := newEnv(t)
		a := e.app()
		e.link(a, "promo", "/product/42")
		e.mobileTap("/promo", uaIOSSafari)
		ts := strconv.FormatInt(time.Now().Add(off).UnixMilli(), 10)
		body := regexp.MustCompile(`"timestamp":\d+`).ReplaceAllString(iosFingerprint(""), `"timestamp":`+ts)
		r := e.sdk(a, "/api/link/match-link", body)
		must(t, r.Code == http.StatusOK && sdkLink(t, r) == sdkRoute(a, "/product/42"), "%s: status %d body %q", name, r.Code, r.Body)
	}
}
