import { describe, expect, it } from 'bun:test';
import { highlightHtml } from '../src/components/code-editor';

// highlighted output goes in via innerHTML: must be the exact source, escaped, only our spans added
const text = (h: string) =>
  h
    .replace(/<\/?span[^>]*>/g, '')
    .replace(/&lt;/g, '<')
    .replace(/&gt;/g, '>')
    .replace(/&amp;/g, '&');

describe('highlightHtml', () => {
  const samples = [
    '<!doctype html>\n<html lang="en"><head><style>\n:root { color-scheme: light dark; }\n@media (x: y) { a { b: c } }\n/* note */\n</style></head>',
    '<body><img class="icon" src="{{icon_url}}" alt="">{{cta}}<!-- hi --></body>',
    "<img src=x onerror=alert(1)><a href='?a=1&b=2'>&amp; {{app_name}}</a>",
    'unclosed <div class="x',
    '<style>a{b:c',
    'plain & text < > {{steps}}',
    '',
  ];
  it('keeps the source exactly', () => {
    for (const s of samples) expect(text(highlightHtml(s))).toBe(s);
  });
  it('never emits a raw tag from the source', () => {
    for (const s of samples) expect(highlightHtml(s).replace(/<\/?span[^>]*>/g, '')).not.toMatch(/[<>]/);
  });
  it('marks tags, attributes, CSS and placeholders', () => {
    const h = highlightHtml('<style>a { color: red }</style><img src="{{icon_url}}">{{cta}}');
    for (const c of ['hl-tag">img', 'hl-attr">src', 'hl-sel">a ', 'hl-prop">color', 'hl-ph">{{icon_url}}', 'hl-ph">{{cta}}'])
      expect(h).toContain(c);
  });
});
