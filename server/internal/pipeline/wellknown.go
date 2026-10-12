package pipeline

// Well-known hosting: per-app association files (AASA for iOS Universal Links,
// assetlinks.json for Android App Links) on operator's configured domains.
// Other hosts 404.

import (
	"net"
	"net/http"
	"strings"

	"detur.dev/server/internal/httpx"
	"detur.dev/server/internal/store"
)

// wellKnownServer: .well-known routes.
type wellKnownServer struct {
	st      *store.Store
	domains []string
}

// RegisterWellKnown: well-known routes on mux; literal patterns beat GET /{key}.
// domains = config.DomainSet; other hosts 404, so one instance never serves
// another operator's domain.
func RegisterWellKnown(mux *http.ServeMux, st *store.Store, domains []string) {
	w := &wellKnownServer{st: st, domains: domains}
	mux.HandleFunc("GET /.well-known/apple-app-site-association", w.handleAASA)
	mux.HandleFunc("GET /.well-known/assetlinks.json", w.handleAssetLinks)
}

// aasaPayload: AASA shape, one details entry per app with ios_app_id; apps always empty array
type aasaPayload struct {
	AppLinks aasaAppLinks `json:"applinks"`
}

type aasaAppLinks struct {
	Apps    []string      `json:"apps"`
	Details []aasaDetails `json:"details"`
}

// aasaDetails: both AASA formats: appID+paths (iOS <= 12), appIDs+components (iOS 13+)
type aasaDetails struct {
	AppID      string              `json:"appID"`
	Paths      []string            `json:"paths"`
	AppIDs     []string            `json:"appIDs"`
	Components []map[string]string `json:"components"`
}

// assetlinkEntry: one Android assetlinks.json entry.
type assetlinkEntry struct {
	Relation []string        `json:"relation"`
	Target   assetlinkTarget `json:"target"`
}

type assetlinkTarget struct {
	Namespace              string   `json:"namespace"`
	PackageName            string   `json:"package_name"`
	SHA256CertFingerprints []string `json:"sha256_cert_fingerprints"`
}

// handleAASA: iOS association file, one details entry per app with ios_app_id.
// One iOS app claims every path; several -> each claims own link keys (iOS routes URL to first matching entry).
func (w *wellKnownServer) handleAASA(rw http.ResponseWriter, r *http.Request) {
	if !w.allowedHost(r) {
		http.NotFound(rw, r)
		return
	}
	apps, err := w.st.ListApps()
	if err != nil {
		http.Error(rw, "internal error", http.StatusInternalServerError)
		return
	}
	var iosApps []store.App
	for _, a := range apps {
		if a.IOSAppID != "" {
			iosApps = append(iosApps, a)
		}
	}
	payload := aasaPayload{AppLinks: aasaAppLinks{Apps: []string{}, Details: []aasaDetails{}}}
	for _, a := range iosApps {
		paths := []string{"*"}
		if len(iosApps) > 1 {
			links, err := w.st.ListLinks(a.ID)
			if err != nil {
				http.Error(rw, "internal error", http.StatusInternalServerError)
				return
			}
			paths = paths[:0]
			for _, l := range links {
				paths = append(paths, "/"+l.Key)
			}
		}
		d := aasaDetails{AppID: a.IOSAppID, Paths: paths, AppIDs: []string{a.IOSAppID}, Components: []map[string]string{}}
		for _, p := range paths {
			d.Components = append(d.Components, map[string]string{"/": p})
		}
		payload.AppLinks.Details = append(payload.AppLinks.Details, d)
	}
	httpx.WriteJSON(rw, http.StatusOK, payload)
}

// handleAssetLinks: Android association file, one entry per app with
// android_package AND cert fingerprint; fingerprint may hold several
// comma-separated values (upload key + Play app signing key).
func (w *wellKnownServer) handleAssetLinks(rw http.ResponseWriter, r *http.Request) {
	if !w.allowedHost(r) {
		http.NotFound(rw, r)
		return
	}
	apps, err := w.st.ListApps()
	if err != nil {
		http.Error(rw, "internal error", http.StatusInternalServerError)
		return
	}
	entries := []assetlinkEntry{}
	for _, a := range apps {
		if a.AndroidPackage == "" || a.AndroidCertFingerprint == "" {
			continue
		}
		entries = append(entries, assetlinkEntry{
			Relation: []string{"delegate_permission/common.handle_all_urls"},
			Target: assetlinkTarget{
				Namespace:              "android_app",
				PackageName:            a.AndroidPackage,
				SHA256CertFingerprints: splitList(a.AndroidCertFingerprint),
			},
		})
	}
	httpx.WriteJSON(rw, http.StatusOK, entries)
}

// splitList: comma-separated value, blanks dropped.
func splitList(s string) []string {
	out := []string{}
	for _, v := range strings.Split(s, ",") {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return out
}

// allowedHost: request Host in configured domain set? Port + leading "www." stripped (Dub parse.ts); case-insensitive.
func (w *wellKnownServer) allowedHost(r *http.Request) bool {
	host := strings.TrimPrefix(strings.ToLower(hostnameOnly(r.Host)), "www.")
	for _, d := range w.domains {
		if host == strings.TrimPrefix(strings.ToLower(hostnameOnly(d)), "www.") {
			return true
		}
	}
	return false
}

// hostnameOnly: Host minus port (and IPv6 brackets).
func hostnameOnly(h string) string {
	if host, _, err := net.SplitHostPort(h); err == nil {
		return host
	}
	return h
}
