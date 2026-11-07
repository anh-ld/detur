// any MCP client speaking Streamable HTTP: URL + bearer header; most read an mcp.json-style config
export const mcpJson = (url: string, token: string) =>
  JSON.stringify({ mcpServers: { detur: { type: 'http', url, headers: { Authorization: `Bearer ${token}` } } } }, null, 2);

export const mcpUrl = (base: string) => `${base}/api/mcp`;
