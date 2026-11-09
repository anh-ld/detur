package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"detur.dev/server/internal/store"
)

func TestMCP(t *testing.T) {
	_, pub, st := newDataEnv(t, "pw")
	a, _ := setup(t, st)
	b, _ := st.CreateApp("other", "k2")
	_, plain, _ := st.CreateToken("agent", []string{a.ID})
	h := bearer(plain)

	rpc := func(body string) (int, map[string]any) {
		resp, raw := portalReq(t, pub, "POST", "/api/mcp", body, h)
		var out map[string]any
		if len(raw) > 0 {
			mustJSON(t, raw, &out)
		}
		return resp.StatusCode, out
	}
	errCode := func(m map[string]any) float64 {
		e, _ := m["error"].(map[string]any)
		c, _ := e["code"].(float64)
		return c
	}
	call := func(tool string, args map[string]any) map[string]any {
		p, _ := json.Marshal(map[string]any{"name": tool, "arguments": args})
		_, m := rpc(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":` + string(p) + `}`)
		return m
	}
	text := func(m map[string]any) (string, bool) {
		r, _ := m["result"].(map[string]any)
		c, _ := r["content"].([]any)
		if len(c) != 1 {
			t.Fatalf("content = %v", m)
		}
		isErr, _ := r["isError"].(bool)
		return c[0].(map[string]any)["text"].(string), isErr
	}

	t.Run("initialize", func(t *testing.T) {
		_, m := rpc(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`)
		r := m["result"].(map[string]any)
		if r["protocolVersion"] != "2025-06-18" || r["capabilities"].(map[string]any)["tools"] == nil {
			t.Errorf("initialize = %v", m)
		}
		_, m = rpc(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"1999-01-01"}}`)
		if m["result"].(map[string]any)["protocolVersion"] != "2025-11-25" {
			t.Errorf("unknown version = %v", m)
		}
	})

	t.Run("notification", func(t *testing.T) {
		if c, _ := rpc(`{"jsonrpc":"2.0","method":"notifications/initialized"}`); c != http.StatusAccepted {
			t.Errorf("notification = %d; want 202", c)
		}
	})

	t.Run("AE5 tools/list read-only", func(t *testing.T) {
		_, m := rpc(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
		tools := m["result"].(map[string]any)["tools"].([]any)
		if len(tools) != 9 {
			t.Fatalf("tools = %d; want 9", len(tools))
		}
		for _, x := range tools {
			n := x.(map[string]any)["name"].(string)
			for arg, p := range x.(map[string]any)["inputSchema"].(map[string]any)["properties"].(map[string]any) {
				if d, _ := p.(map[string]any)["description"].(string); d == "" {
					t.Errorf("%s.%s: empty description", n, arg)
				}
			}
			if strings.HasPrefix(n, "create") || strings.HasPrefix(n, "update") || strings.HasPrefix(n, "delete") || strings.HasPrefix(n, "set") {
				t.Errorf("write tool %s", n)
			}
		}
	})

	t.Run("tools/call", func(t *testing.T) {
		txt, isErr := text(call("list_apps", nil))
		if isErr || !strings.Contains(txt, a.ID) || strings.Contains(txt, b.ID) {
			t.Errorf("list_apps = %v %s; want only A", isErr, txt)
		}
		txt, isErr = text(call("get_analytics", map[string]any{"app_id": a.ID, "days": 7}))
		if isErr || !strings.Contains(txt, `"days"`) {
			t.Errorf("get_analytics A = %v %s", isErr, txt)
		}
		if txt, isErr = text(call("get_analytics", map[string]any{"app_id": b.ID})); !isErr || txt != "not found" {
			t.Errorf("get_analytics B = %v %q; want isError not found", isErr, txt)
		}
		if _, isErr = text(call("get_analytics", map[string]any{"app_id": a.ID, "platform": "tv"})); !isErr {
			t.Errorf("bad platform should be isError")
		}
	})

	t.Run("errors", func(t *testing.T) {
		if errCode(call("create_link", nil)) != -32602 || errCode(call("get_app", nil)) != -32602 {
			t.Errorf("unknown tool / missing app_id want -32602")
		}
		if _, m := rpc(`{"jsonrpc":"2.0","id":1,"method":"resources/list"}`); errCode(m) != -32601 {
			t.Errorf("unknown method = %v", m)
		}
		if _, m := rpc(`{nope`); errCode(m) != -32700 {
			t.Errorf("malformed = %v", m)
		}
	})

	t.Run("transport", func(t *testing.T) {
		if resp, _ := portalReq(t, pub, "POST", "/api/mcp", `{}`, nil); resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("no token = %d; want 401", resp.StatusCode)
		}
		for _, m := range []string{"GET", "DELETE"} {
			if resp, _ := portalReq(t, pub, m, "/api/mcp", "", h); resp.StatusCode != http.StatusMethodNotAllowed {
				t.Errorf("%s = %d; want 405", m, resp.StatusCode)
			}
		}
		if _, err := st.CreateLink(store.Link{AppID: a.ID, Key: "mcp", URL: "https://example.com/m"}); err != nil {
			t.Fatal(err)
		}
		noFollow := *http.DefaultClient
		noFollow.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
		resp, err := noFollow.Get(pub.URL + "/mcp")
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode < 300 || resp.StatusCode > 399 {
			t.Errorf("GET /mcp = %d; want redirect", resp.StatusCode)
		}
	})
}
