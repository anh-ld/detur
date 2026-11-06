package api

// MCP: stateless Streamable HTTP, JSON responses only (no SSE, no session). Tools are the data API reads,
// dispatched in-process to the /api/v1 mux with the caller's token, so MCP can never see more than HTTP does.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"

	"detur.dev/server/internal/httpx"
)

const mcpLatest = "2025-11-25"

var mcpVersions = []string{mcpLatest, "2025-06-18", "2025-03-26"}

type mcpTool struct {
	name, desc, path string   // path: /api/v1 template, {x} = required string arg
	query            []string // optional args passed as query params
}

const rangeDesc = " Range: days (UTC days ending today) or from+to (YYYY-MM-DD, UTC)."

var mcpTools = []mcpTool{
	{"list_apps", "List the apps this token can read. Call first to get app ids.", "/api/v1/apps", nil},
	{"get_app", "App info: name, iOS app id, Android package, iOS scheme.", "/api/v1/apps/{app_id}", nil},
	{"list_links", "Short links of an app: key, destinations, fallback, expiry, tags.", "/api/v1/apps/{app_id}/links", nil},
	{"get_link_rules", "Routing rules of a link, in evaluation order.", "/api/v1/apps/{app_id}/links/{link_id}/rules", nil},
	{"get_link_variants", "Per-variant clicks and installs of a link (A/B split)." + rangeDesc + " Default 7 days.", "/api/v1/apps/{app_id}/links/{link_id}/variants", []string{"days", "from", "to"}},
	{"get_analytics", "Daily clicks, installs, events, conversions, D1/D7/D30 retention, per-link and per-source totals." + rangeDesc + " Default 7 days. Supports window (15m, 1h, 3h, 12h) and link_id.", "/api/v1/apps/{app_id}/analytics", []string{"days", "from", "to", "platform", "window", "link_id"}},
	{"get_match_quality", "Install match quality: methods, score distribution." + rangeDesc + " Default 30 days.", "/api/v1/apps/{app_id}/match-quality", []string{"days", "from", "to"}},
	{"get_fraud", "Fraud signal counts (velocity, timing, user agent, IP)." + rangeDesc + " Default 7 days.", "/api/v1/apps/{app_id}/fraud", []string{"days", "from", "to"}},
	{"get_health", "Setup checks for an app: SDK seen, iOS app id, Android, AASA.", "/api/v1/apps/{app_id}/health", nil},
}

var argDesc = map[string]string{
	"app_id": "App id from list_apps.", "link_id": "Link id from list_links.",
	"days": "Last N UTC days, ending today.", "from": "Start day, YYYY-MM-DD UTC (use with to).",
	"to": "End day, YYYY-MM-DD UTC (use with from).", "platform": "ios, android or desktop. Omit for all.",
}

// pathArgs: {x} names in the template, in order.
func (t mcpTool) pathArgs() []string {
	var out []string
	for _, seg := range strings.Split(t.path, "/") {
		if strings.HasPrefix(seg, "{") {
			out = append(out, seg[1:len(seg)-1])
		}
	}
	return out
}

func (t mcpTool) schema() map[string]any {
	props := map[string]any{}
	req := []string{}
	for _, a := range append(t.pathArgs(), t.query...) {
		typ := "string"
		if a == "days" {
			typ = "integer"
		}
		props[a] = map[string]any{"type": typ, "description": argDesc[a]}
	}
	req = append(req, t.pathArgs()...)
	return map[string]any{"type": "object", "properties": props, "required": req}
}

type rpcReq struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

func rpcOK(w http.ResponseWriter, id json.RawMessage, result any) {
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
}

func rpcErr(w http.ResponseWriter, id json.RawMessage, code int, msg string) {
	if len(id) == 0 {
		id = json.RawMessage("null")
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": code, "message": msg}})
}

// mcp: one JSON-RPC message per POST (token already checked by auth).
func (d *dataServer) mcp(w http.ResponseWriter, r *http.Request) {
	var req rpcReq
	if err := decodeJSON(w, r, &req); err != nil {
		rpcErr(w, nil, -32700, "parse error")
		return
	}
	if len(req.ID) == 0 { // notification: no response body
		w.WriteHeader(http.StatusAccepted)
		return
	}
	switch req.Method {
	case "initialize":
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		_ = json.Unmarshal(req.Params, &p)
		v := mcpLatest
		if slices.Contains(mcpVersions, p.ProtocolVersion) {
			v = p.ProtocolVersion
		}
		rpcOK(w, req.ID, map[string]any{
			"protocolVersion": v,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "detur", "version": "dev"},
			"instructions":    "Read-only detur analytics. Call list_apps first.",
		})
	case "ping":
		rpcOK(w, req.ID, map[string]any{})
	case "tools/list":
		tools := make([]map[string]any, 0, len(mcpTools))
		for _, t := range mcpTools {
			tools = append(tools, map[string]any{"name": t.name, "description": t.desc, "inputSchema": t.schema()})
		}
		rpcOK(w, req.ID, map[string]any{"tools": tools})
	case "tools/call":
		d.mcpCall(w, r, req)
	default:
		rpcErr(w, req.ID, -32601, "method not found")
	}
}

func (d *dataServer) mcpCall(w http.ResponseWriter, r *http.Request, req rpcReq) {
	var p struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &p); err != nil {
		rpcErr(w, req.ID, -32602, "invalid params")
		return
	}
	i := slices.IndexFunc(mcpTools, func(t mcpTool) bool { return t.name == p.Name })
	if i < 0 {
		rpcErr(w, req.ID, -32602, "unknown tool: "+p.Name)
		return
	}
	t := mcpTools[i]
	path := t.path
	for _, a := range t.pathArgs() {
		v, ok := p.Arguments[a].(string)
		if !ok || v == "" {
			rpcErr(w, req.ID, -32602, a+" is required")
			return
		}
		path = strings.ReplaceAll(path, "{"+a+"}", url.PathEscape(v))
	}
	q := url.Values{}
	for _, a := range t.query {
		if v, ok := p.Arguments[a]; ok && v != nil {
			q.Set(a, fmt.Sprint(v))
		}
	}
	if len(q) > 0 {
		path += "?" + q.Encode()
	}
	call, err := http.NewRequestWithContext(r.Context(), http.MethodGet, path, nil)
	if err != nil {
		rpcErr(w, req.ID, -32602, "invalid params")
		return
	}
	call.Header.Set("Authorization", r.Header.Get("Authorization"))
	rec := httptest.NewRecorder()
	d.v1.ServeHTTP(rec, call)
	rpcOK(w, req.ID, map[string]any{
		"content": []map[string]any{{"type": "text", "text": strings.TrimSpace(rec.Body.String())}},
		"isError": rec.Code >= 300,
	})
}
