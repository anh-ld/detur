package scenarios

import "testing"

func TestS_SMOKE_01_HarnessBoots(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.link(a, "abc", "/p")
	r := do(t, "GET", e.Pub.URL+"/health", "", nil)
	must(t, r.Code == 200, "health = %d", r.Code)
	r = e.portal("GET", "/api/apps", "", nil)
	must(t, r.Code == 200, "portal = %d %s", r.Code, r.Body)
}
