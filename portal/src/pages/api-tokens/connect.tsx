import { useState } from 'preact/hooks';
import { Toggle, ToggleGroup } from 'kinu';
import { muted } from '../../lib/styles';
import { mcpJson, mcpUrl } from './mcp';
import { Snippet } from './snippet';

// how-to, agent-agnostic: placeholder token; real token only in the created dialog.
// One tab at a time: the page stays short, each tab is one copy-paste job.
export function Connect({ base }: { base: string }) {
  const url = mcpUrl(base);
  const tabs = [
    {
      label: 'MCP',
      hint: 'Any remote (Streamable HTTP) MCP client: URL + header.',
      snippets: [
        { label: 'MCP URL', value: url },
        { label: 'Header', value: 'Authorization: Bearer <token>' },
      ],
    },
    {
      label: 'mcp.json',
      hint: 'Claude Code and Cursor. VS Code uses a "servers" key, Codex its config.toml.',
      snippets: [{ label: 'mcp.json', value: mcpJson(url, '<token>') }],
    },
    {
      label: 'HTTP API',
      hint: 'Plain REST, same token.',
      snippets: [{ label: 'curl', value: `curl -H "Authorization: Bearer <token>" "${base}/api/v1/apps"` }],
    },
  ];
  const [i, setI] = useState(0);
  const tab = tabs[i];
  return (
    <section style={{ display: 'grid', gap: 12 }}>
      <div style={{ display: 'grid', gap: 4 }}>
        <h2 style={{ margin: 0, fontSize: 20 }}>Connect</h2>
        <p style={muted}>
          Create a token, add detur to your agent, then ask "Which links drove installs last week?". Read-only: apps, links, rules,
          variants, analytics, match quality, fraud counts, health.
        </p>
      </div>
      <ToggleGroup role="group" aria-label="Client" class="seg" style={{ justifySelf: 'start' }}>
        {tabs.map((t, n) => (
          <Toggle key={t.label} type="button" size="sm" pressed={i === n} onClickCapture={() => {}} onClick={() => setI(n)}>
            {t.label}
          </Toggle>
        ))}
      </ToggleGroup>
      <p style={muted}>{tab.hint}</p>
      {tab.snippets.map((s) => (
        <Snippet key={s.label} icon label={s.label} value={s.value} copyLabel={`Copy ${s.label}`} />
      ))}
    </section>
  );
}
