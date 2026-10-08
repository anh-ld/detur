// Shared integration-test scaffolding: real Go binary + fresh SQLite per file.
// flows.test.tsx (AE1: ADMIN_PASSWORD unset) and admin.test.tsx (gated) each own a
// server lifecycle; vitest runs files sequentially (fileParallelism: false), so the
// :8080 SDK port and the per-file portal ports never collide.
import { execSync, spawn, ChildProcess } from 'node:child_process';
import { mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';
import { screen } from '@testing-library/preact';

const repoRoot = resolve(process.cwd(), '..'); // vitest runs from portal/

export async function waitForServer(
  url: string,
  ms: number,
  server: ChildProcess,
  getStderr: () => string,
): Promise<void> {
  const start = Date.now();
  for (;;) {
    if (server.exitCode !== null) {
      throw new Error(`detur exited early (${server.exitCode}): ${getStderr()}`);
    }
    try {
      const res = await fetch(url);
      if (res.ok) return;
    } catch {
      // not up yet
    }
    if (Date.now() - start > ms) {
      throw new Error(`server not ready at ${url}: ${getStderr()}`);
    }
    await new Promise((r) => setTimeout(r, 300));
  }
}

// Open <dialog>: kinu shows one modal at a time.
export const openDialog = (): HTMLElement => {
  const d = document.querySelector('dialog[open]');
  if (!d) throw new Error('no open dialog');
  return d as HTMLElement;
};

export const uniq = (s: string) => `${s}-${Date.now().toString(36)}`;

// Navigate like the address bar does; the explicit event covers DOMs that don't fire hashchange on assignment.
export function go(hash: string) {
  location.hash = hash;
  window.dispatchEvent(new Event('hashchange'));
}

export const tabs = () => screen.getAllByRole('tab');
export const selectedTab = () =>
  tabs()
    .filter((t) => t.getAttribute('aria-selected') === 'true')
    .map((t) => t.textContent);

// Build + spawn one server instance with a fresh SQLite DB and a unique portal port.
export function startServer(opts: { port: number; bin: string; env?: Record<string, string> }) {
  execSync(`go build -o ${opts.bin} ./cmd/detur`, { cwd: join(repoRoot, 'server'), stdio: 'inherit' });
  const dbDir = mkdtempSync(join(tmpdir(), 'detur-integ-'));
  let stderr = '';
  const server = spawn(opts.bin, ['-portal-addr', `127.0.0.1:${opts.port}`], {
    cwd: repoRoot,
    env: { ...process.env, DB_PATH: join(dbDir, 'test.db'), ...opts.env },
    stdio: ['ignore', 'ignore', 'pipe'],
  });
  server.stderr!.on('data', (d: Buffer) => {
    stderr += d.toString();
  });
  return { server, dbDir, getStderr: () => stderr };
}

export function stopServer(server: ChildProcess, dbDir: string, bin: string) {
  server?.kill();
  if (dbDir) rmSync(dbDir, { recursive: true, force: true });
  rmSync(bin, { force: true });
}