package api

// Health: per-app setup checks. Local data only, no outbound calls.

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"detur.dev/server/internal/httpx"
	"detur.dev/server/internal/store"
)

type healthCheck struct {
	Check  string `json:"check"`
	Status string `json:"status"` // ok | warn | fail
	Detail string `json:"detail"`
}

var (
	iosAppIDRe    = regexp.MustCompile(`^[A-Z0-9]{10}\.[A-Za-z0-9.-]+$`)
	androidPkgRe  = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*(\.[A-Za-z][A-Za-z0-9_]*)+$`)
	fingerprintRe = regexp.MustCompile(`^([0-9A-Fa-f]{2}:){31}[0-9A-Fa-f]{2}$`)
)

// aasaLag: Apple CDN AASA cache; newer links may open the browser.
const aasaLag = 48 * time.Hour

// minPeers: proxy check needs this many peers.
const minPeers = 20

// health: GET /api/apps/{id}/health.
func (p *portalServer) health(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	app, err := p.st.GetApp(id)
	if err != nil {
		p.storeErr(w, err)
		return
	}
	checks, err := p.healthChecks(app, time.Now())
	if err != nil {
		p.internal(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, checks)
}

func (p *portalServer) healthChecks(app store.App, now time.Time) ([]healthCheck, error) {
	var out []healthCheck
	add := func(check, status, detail string) { out = append(out, healthCheck{check, status, detail}) }

	version, seen, err := p.st.SDKSeen(app.ID)
	if err != nil {
		return nil, err
	}
	if seen.IsZero() {
		add("SDK", "fail", "No SDK call yet. Patch applied? Base URL right?")
	} else {
		add("SDK", "ok", fmt.Sprintf("%s, last call %s", version, seen.Format("2006-01-02 15:04 UTC")))
	}

	switch {
	case app.IOSAppID == "":
		add("iOS app ID", "warn", "Not set. Universal Links off.")
	case !iosAppIDRe.MatchString(app.IOSAppID):
		add("iOS app ID", "fail", "Expected TEAMID.BUNDLEID, e.g. ABCDE12345.com.example.app.")
	default:
		add("iOS app ID", "ok", app.IOSAppID)
	}

	pkg, fp := app.AndroidPackage, app.AndroidCertFingerprint
	switch {
	case pkg == "" && fp == "":
		add("Android", "warn", "Not set. App Links off.")
	case pkg == "" || fp == "":
		add("Android", "fail", "Package and cert fingerprint both needed for assetlinks.json.")
	case !androidPkgRe.MatchString(pkg):
		add("Android", "fail", "Package should look like com.example.app.")
	case !allMatch(fp, fingerprintRe):
		add("Android", "fail", "Fingerprint should be SHA-256, 32 hex pairs: AA:BB:…")
	default:
		add("Android", "ok", pkg)
	}

	// >1 iOS app: per-app paths, new links wait for AASA refresh.
	if app.IOSAppID != "" {
		apps, err := p.st.ListApps()
		if err != nil {
			return nil, err
		}
		ios := 0
		for _, a := range apps {
			if a.IOSAppID != "" {
				ios++
			}
		}
		if ios > 1 {
			keys, err := p.st.RecentLinkKeys(app.ID, now.Add(-aasaLag))
			if err != nil {
				return nil, err
			}
			if len(keys) > 0 {
				add("AASA refresh", "warn", fmt.Sprintf("%d link(s) from the last 48h may open the browser until Apple refreshes: /%s", len(keys), strings.Join(keys, ", /")))
			} else {
				add("AASA refresh", "ok", "No links newer than 48h.")
			}
		}
	}

	total, private := httpx.PeerStats()
	switch {
	case httpx.TrustProxy:
		add("Proxy", "ok", "TRUST_PROXY=1: client IP from X-Forwarded-For.")
	case total < minPeers:
		add("Proxy", "ok", "Not enough traffic yet to judge.")
	case private*2 > total:
		add("Proxy", "fail", fmt.Sprintf("%d of %d requests came from private IPs: every click shares one IP. Behind a proxy? Set TRUST_PROXY=1.", private, total))
	default:
		add("Proxy", "ok", "Client IPs look public.")
	}
	return out, nil
}

// allMatch: every comma value matches re.
func allMatch(list string, re *regexp.Regexp) bool {
	for _, v := range strings.Split(list, ",") {
		if !re.MatchString(strings.TrimSpace(v)) {
			return false
		}
	}
	return true
}
