import { JSX } from 'preact';
import { useRef } from 'preact/hooks';

// HTML (+ <style> CSS) highlighter for the page editor. Output = escaped source wrapped in spans; nothing else.
const esc = (s: string) => s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
const span = (cls: string, s: string) => (s ? `<span class="hl-${cls}">${esc(s)}</span>` : '');
const ph = (s: string) => esc(s).replace(/\{\{[a-z_]+\}\}/g, (m) => `<span class="hl-ph">${m}</span>`);

// gaps between matches -> plain(text); matches -> hit(match)
function scan(src: string, re: RegExp, hit: (m: RegExpExecArray) => string, plain: (s: string) => string): string {
  let out = '';
  let at = 0;
  for (let m = re.exec(src); m; m = re.exec(src)) {
    if (!m[0]) {
      re.lastIndex++;
      continue;
    }
    out += plain(src.slice(at, m.index)) + hit(m);
    at = m.index + m[0].length;
  }
  return out + plain(src.slice(at));
}

function css(src: string): string {
  return scan(
    src,
    /(\/\*[\s\S]*?(?:\*\/|$))|([^{};]+)(?=\{)|([\w-]+)(\s*:)([^;{}]*)/g,
    (m) =>
      m[1] ? span('comment', m[1]) : m[2] ? span('sel', m[2]) : span('prop', m[3]) + span('punct', m[4]) + `<span class="hl-val">${ph(m[5])}</span>`,
    (s) => span('punct', s),
  );
}

function tag(t: string): string {
  if (/^<!/.test(t)) return span('comment', t);
  const [, open, name, rest] = /^(<\/?)([^\s/>]*)([\s\S]*)$/.exec(t)!;
  return (
    span('punct', open) +
    span('tag', name) +
    scan(
      rest,
      /([^\s=/>"']+)|("[^"]*"?|'[^']*'?)/g,
      (m) => (m[1] ? span('attr', m[1]) : `<span class="hl-str">${ph(m[2])}</span>`),
      (s) => span('punct', s),
    )
  );
}

export function highlightHtml(src: string): string {
  return scan(
    src,
    /(<!--[\s\S]*?(?:-->|$))|(<style\b[^>]*>)([\s\S]*?)(<\/style\s*>|$)|(<[!/]?[a-zA-Z][^>]*>?)/gi,
    (m) => (m[1] ? span('comment', m[1]) : m[2] ? tag(m[2]) + css(m[3]) + (m[4] ? tag(m[4]) : '') : tag(m[5])),
    ph,
  );
}

// transparent textarea over a highlighted <pre>: native editing, undo, selection; pre follows its scroll
export function CodeEditor({ id, value, onInput, height }: { id: string; value: string; onInput: (v: string) => void; height: number }) {
  const pre = useRef<HTMLPreElement>(null);
  const sync = (e: JSX.TargetedEvent<HTMLTextAreaElement>) => {
    if (!pre.current) return;
    pre.current.scrollTop = e.currentTarget.scrollTop;
    pre.current.scrollLeft = e.currentTarget.scrollLeft;
  };
  // Tab indents 2 spaces (Esc then Tab still leaves the field: Tab only inserts while no modifier)
  const key = (e: JSX.TargetedKeyboardEvent<HTMLTextAreaElement>) => {
    if (e.key !== 'Tab' || e.shiftKey || e.altKey || e.ctrlKey || e.metaKey) return;
    e.preventDefault();
    const t = e.currentTarget;
    t.setRangeText('  ', t.selectionStart, t.selectionEnd, 'end');
    onInput(t.value);
  };
  return (
    <div class="code-editor" style={{ height }}>
      {/* trailing newline: pre keeps the textarea's last empty line */}
      <pre ref={pre} aria-hidden="true" dangerouslySetInnerHTML={{ __html: highlightHtml(value) + '\n' }} />
      <textarea
        id={id}
        value={value}
        spellcheck={false}
        autocapitalize="off"
        autocomplete="off"
        wrap="off"
        onInput={(e) => onInput(e.currentTarget.value)}
        onScroll={sync}
        onKeyDown={key}
      />
    </div>
  );
}
