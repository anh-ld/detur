// real Go binary + fresh SQLite per file. flows (AE1: ADMIN_PASSWORD unset) and admin (gated)
// each own their server.
import { execSync, spawn, ChildProcess } from 'node:child_process';
import { mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';
import { expect } from 'bun:test';
import { screen, within } from '@testing-library/preact';

const repoRoot = resolve(import.meta.dir, '../..');

export async function waitForServer(url: string, ms: number, server: ChildProcess, getStderr: () => string): Promise<void> {
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

// open <dialog>; kinu shows one modal at a time
export const openDialog = (): HTMLElement => {
  const d = document.querySelector('dialog[open]');
  if (!d) throw new Error('no open dialog');
  return d as HTMLElement;
};

export const uniq = (s: string) => `${s}-${Date.now().toString(36)}`;

// Settings save buttons all read "Save": scope to the form (or toolbar) that owns the anchor.
export const saveButton = (anchor: Element) =>
  within((anchor.closest('form') ?? anchor.parentElement) as HTMLElement).getByRole('button', { name: 'Save' });

// navigate like the address bar; explicit event for DOMs that skip hashchange on assignment
export function go(hash: string) {
  location.hash = hash;
  window.dispatchEvent(new Event('hashchange'));
}

// sidebar nav links; text minus the <kbd> shortcut hint
const navText = (a: HTMLElement) =>
  a.firstChild
    ? [...a.childNodes]
        .filter((n) => n.nodeName !== 'KBD')
        .map((n) => n.textContent)
        .join('')
    : '';
export const tabs = () => within(screen.getByRole('navigation', { name: 'App sections' })).getAllByRole('link');
export const tabNames = () => tabs().map(navText);
export const selectedTab = () =>
  tabs()
    .filter((t) => t.getAttribute('aria-current') === 'page')
    .map(navText);

// build + spawn server: fresh SQLite, unique portal port
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

// expect(x).toBeNull() for DOM nodes: bun formats a failing node eagerly (millions of lines, ~1s, blocks the
// event loop inside waitFor), vitest only on the final failure. Print the tag instead.
export function gone(el: unknown): void {
  expect(el instanceof Element ? el.outerHTML.slice(0, 200) : el).toBeNull();
}
