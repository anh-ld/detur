package pipeline

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

var reCopyToken = regexp.MustCompile(`"\?_dc=([0-9A-Za-z]{16})"`)

// Copy page mints a paste token; the reload's pasted_link carries it onto the click; `_dc` never forwarded.
func TestCopyPagePasteToken(t *testing.T) {
	ts, s, _ := newPipelineServer(t)
	app, link := setupPipeline(t, s)
	_, body := doGET(t, ts, "/"+link.Key, map[string]string{"User-Agent": iosUA})
	m := reCopyToken.FindStringSubmatch(string(body))
	if m == nil {
		t.Fatalf("copy page: no paste token:\n%s", body)
	}
	tok := m[1]
	pasted := ts.URL + "/" + link.Key + "?_dc=" + tok
	resp, _ := doGET(t, ts, "/"+link.Key+"?_dt=1&_dc="+tok+"&pasted_link="+url.QueryEscape(pasted), map[string]string{"User-Agent": iosUA})
	if resp.StatusCode != http.StatusFound || strings.Contains(resp.Header.Get("Location"), "_dc") {
		t.Fatalf("reload = %d %q; want 302 without _dc", resp.StatusCode, resp.Header.Get("Location"))
	}
	c, err := s.ClickByPasteToken(app.ID, link.Key, tok, "dev")
	if err != nil || c.Destination != link.URL || strings.Contains(c.Destination, "_dc") {
		t.Fatalf("token click = %+v, %v", c, err)
	}
}

// iOS tap page copies a link with its own token.
func TestTapPagePasteToken(t *testing.T) {
	ts, s, _ := newPipelineServer(t)
	_, link := setupPipeline(t, s)
	_, body := doGET(t, ts, "/"+link.Key+"?_dt=1", map[string]string{"User-Agent": messengerIOSUA})
	if !reCopyToken.Match(body) {
		t.Fatalf("tap page: no paste token:\n%s", body)
	}
}
