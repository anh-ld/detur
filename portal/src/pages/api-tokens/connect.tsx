import { Card } from 'kinu';
import { muted } from '../../lib/styles';
import { mcpJson, mcpUrl } from './mcp';
import { Snippet } from './snippet';

// how-to, agent-agnostic: placeholder token; real token only in the created dialog
export function Connect({ base }: { base: string }) {
  const url = mcpUrl(base);
  const snippets = [
    { label: 'MCP URL (Streamable HTTP)', value: url },
    { label: 'Header', value: 'Authorization: Bearer <token>' },
    { label: 'mcp.json (Claude Code, Cursor)', value: mcpJson(url, '<token>') },
    { label: 'HTTP API', value: `curl -H "Authorization: Bearer <token>" "${base}/api/v1/apps"` },
  ];
  return (
    <Card>
      <div style={{ display: 'grid', gap: 16, padding: 16 }}>
        <h2 style={{ margin: 0, fontSize: 20 }}>Connect</h2>
        <ol style={{ ...muted, paddingLeft: 20, display: 'grid', gap: 4 }}>
          <li>Create a token below and pick its apps.</li>
          <li>
            Add detur as a remote (Streamable HTTP) MCP server in your agent: URL + header works everywhere; the mcp.json block fits Claude
            Code and Cursor (VS Code uses a "servers" key, Codex its config.toml).
          </li>
          <li>Ask: "Which links drove installs last week?" Agents start with list_apps.</li>
        </ol>
        {snippets.map((s) => (
          <Snippet key={s.label} icon label={s.label} value={s.value} copyLabel={`Copy ${s.label}`} />
        ))}
        <p style={muted}>Read-only: apps, links, rules, variants, analytics, match quality, fraud counts, health.</p>
      </div>
    </Card>
  );
}
