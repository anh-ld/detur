import { useState } from 'preact/hooks';
import { api } from '../../../lib/api';
import type { LinkRule } from '../../../lib/types';
import { useLatest } from '../../../lib/use-latest';

// Rules for one link: load, local edits (dirty until saved), save.
export function useLinkRules(appId: string, linkId: string, setErr: (msg: string) => void) {
  const [rules, setRules] = useState<LinkRule[]>([]);
  const [loading, setLoading] = useState(false);
  const [loaded, setLoaded] = useState(false); // a failed load leaves rules=[]; block edits so Save can't PUT an empty list
  const [dirty, setDirty] = useState(false);
  const latest = useLatest();

  const load = async () => {
    const live = latest.begin();
    setLoading(true);
    setErr('');
    setLoaded(false);
    try {
      const r = await api.getLinkRules(appId, linkId);
      if (!live()) return;
      setRules(r);
      setLoaded(true);
      setDirty(false);
    } catch (e) {
      if (live()) setErr(String(e));
    } finally {
      if (live()) setLoading(false);
    }
  };

  const change = (next: LinkRule[]) => {
    setRules(next);
    setDirty(true);
  };

  const move = (idx: number, dir: -1 | 1) => {
    const to = idx + dir;
    if (to < 0 || to >= rules.length) return;
    const next = [...rules];
    next.splice(to, 0, ...next.splice(idx, 1));
    change(next);
  };

  const save = async () => {
    setRules(await api.setLinkRules(appId, linkId, rules));
    setDirty(false);
  };

  return { rules, loading, loaded, dirty, load, change, move, remove: (idx: number) => change(rules.filter((_, i) => i !== idx)), save };
}
