package scenarios

import (
	"net/http"
	"os"
	"testing"
)

type storeStub struct{ next http.RoundTripper }

func (s storeStub) RoundTrip(r *http.Request) (*http.Response, error) {
	switch r.URL.Hostname() {
	case "itunes.apple.com", "play.google.com":
		return &http.Response{StatusCode: http.StatusNotFound, Body: http.NoBody, Request: r}, nil
	}
	return s.next.RoundTrip(r)
}

func TestMain(m *testing.M) {
	http.DefaultTransport = storeStub{next: http.DefaultTransport}
	os.Exit(m.Run())
}
