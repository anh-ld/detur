import { fetch as nodeFetch } from 'undici';

// happy-dom fetch enforces CORS; server same-origin by design. Use Node's real fetch.
globalThis.fetch = ((input: any, init?: any) => nodeFetch(input, init)) as unknown as typeof fetch;

// Platform shims happy-dom lacks: clipboard for copy flows, scrollTo for shell.
Object.defineProperty(navigator, 'clipboard', { value: { writeText: async () => {} }, configurable: true });
window.scrollTo = (() => {}) as typeof window.scrollTo;