package pipeline

// Well-known hosting: the platform association files per app, Apple App
// Site Association (iOS Universal Links) and assetlinks.json (Android App
// Links), served under the operator's configured domains. Hosts outside the
// domain set get 404.

import (
	"net"
	"net/http"
	"strings"

	"detur.dev/server/internal/httpx"
	"detur.dev/server/internal/store"
)

// wellKnownServer serves the .well-known routes.
type wellKnownServer struct {
	st      *store.Store
	domains []string
}

// RegisterWellKnown attaches the well-known routes to mux. Their literal
// patterns are more specific than the pipeline's GET /{key}, so they win for
// these paths. domains is the configured domain set (config.DomainSet);
// hosts outside it get 404 so one instance never serves another operator's
// configured domain.
func RegisterWellKnown(mux *http.ServeMux, st *store.Store, domains []string) {
	w := &wellKnownServer{st: st, domains: domains}
	mux.HandleFunc("GET /.well-known/apple-app-site-association", w.handleAASA)
	mux.HandleFunc("GET /.well-known/assetlinks.json", w.handleAssetLinks)
}

// aasaPayload is the Apple App Site Association shape: one details entry
// per app with an ios_app_id; apps always stays an empty array.
type aasaPayload struct {
	AppLinks aasaAppLinks `json:"applinks"`
}

type aasaAppLinks struct {
	Apps    []string      `json:"apps"`
	Details []aasaDetails `json:"details"`
}

type aasaDetails struct {
	AppID string   `json:"appID"`
	Paths []string `json:"paths"`
}

// assetlinkEntry is one Android assetlinks.json entry.
type assetlinkEntry struct {
	Relation []string        `json:"relation"`
	Target   assetlinkTarget `json:"target"`
}

type assetlinkTarget struct {
	Namespace              string   `json:"namespace"`
	PackageName            string   `json:"package_name"`
	SHA256CertFingerprints []string `json:"sha256_cert_fingerprints"`
}

// handleAASA serves the iOS association file: one details entry per app that
// carries an ios_app_id.
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
	payload := aasaPayload{AppLinks: aasaAppLinks{Apps: []string{}, Details: []aasaDetails{}}}
	for _, a := range apps {
		if a.IOSAppID == "" {
			continue
		}
		payload.AppLinks.Details = append(payload.AppLinks.Details, aasaDetails{AppID: a.IOSAppID, Paths: []string{"*"}})
	}
	httpx.WriteJSON(rw, http.StatusOK, payload)
}

// handleAssetLinks serves the Android association file: one entry per app
// that carries an android_package AND a cert fingerprint.
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
				SHA256CertFingerprints: []string{a.AndroidCertFingerprint},
			},
		})
	}
	httpx.WriteJSON(rw, http.StatusOK, entries)
}

// allowedHost reports whether the request Host is in the configured domain
// set. Ports are stripped and comparison is case-insensitive.
func (w *wellKnownServer) allowedHost(r *http.Request) bool {
	host := hostnameOnly(r.Host)
	for _, d := range w.domains {
		if strings.EqualFold(host, hostnameOnly(d)) {
			return true
		}
	}
	return false
}

// hostnameOnly strips the port (and IPv6 brackets) from a Host value.
func hostnameOnly(h string) string {
	if host, _, err := net.SplitHostPort(h); err == nil {
		return host
	}
	return h
}
