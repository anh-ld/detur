export const mcpJson = (url: string, token: string) =>
  JSON.stringify({ mcpServers: { detur: { type: 'http', url, headers: { Authorization: `Bearer ${token}` } } } }, null, 2);

export const mcpUrl = (base: string) => `${base}/api/mcp`;
