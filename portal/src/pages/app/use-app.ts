import { useEffect, useState } from 'preact/hooks';
import { Analytics, App, Link, Platform, VariantStat } from '../../lib/types';
import { api } from '../../lib/api';
import { DateRangeFilter } from '../../lib/range';
import { AppTab } from '../../lib/route';
import { useLatest } from '../../lib/use-latest';

// Loads the app, its links, analytics and variant stats. Stale responses are dropped.
export function useApp(id: string, tab: AppTab, range: DateRangeFilter, platform: Platform, routeLinkId: string) {
  const [app, setApp] = useState<App | null>(null);
  const [links, setLinks] = useState<Link[] | null>(null);
  const [stats, setStats] = useState<Analytics | null>(null);
  const [variants, setVariants] = useState<VariantStat[] | null>(null);
  const [error, setError] = useState('');
  const [statsError, setStatsError] = useState(''); // own slot: a fixed range clears it without hiding an app-load error
  const [statsLoading, setStatsLoading] = useState(false);
  const [linkBase, setLinkBase] = useState('');
  const [tick, setTick] = useState(0);
  const [refreshing, setRefreshing] = useState(false);

  const appLoad = useLatest();
  const statsLoad = useLatest();
  const variantsLoad = useLatest();

  // stale link id (not in loaded links) counts as all links
  const selectedLinkId = links && !links.some((l) => l.id === routeLinkId) ? '' : routeLinkId;
  const filtered = tab === 'links' || tab === 'analytics';

  const load = () => {
    setError('');
    const ok = appLoad.begin();
    return Promise.all([
      api
        .getApp(id)
        .then((a) => ok() && setApp(a))
        .catch((e) => ok() && setError(String(e))),
      api
        .listLinks(id)
        .then((l) => ok() && setLinks(l))
        .catch((e) => ok() && setError(String(e))),
    ]);
  };

  // spin until loaded, min 600ms so fast reloads show
  const refresh = () => {
    setRefreshing(true);
    setTick((t) => t + 1);
    Promise.all([load(), new Promise((r) => setTimeout(r, 600))]).then(() => setRefreshing(false));
  };

  useEffect(() => {
    api.getConfig().then(
      (c) => setLinkBase(c.linkBase ?? ''),
      () => {},
    );
  }, []);

  useEffect(() => {
    setApp(null);
    setLinks(null);
    setStats(null);
    setStatsError('');
    load();
  }, [id]);

  useEffect(() => {
    if (!filtered) return;
    const live = statsLoad.begin();
    setStatsLoading(true);
    api
      .getAnalytics(id, range, platform, tab === 'analytics' ? selectedLinkId : undefined)
      .then((s) => {
        if (!live()) return;
        setStats(s);
        setStatsError('');
      })
      .catch((e) => live() && setStatsError(String(e)))
      .finally(() => live() && setStatsLoading(false));
    return () => {
      statsLoad.cancel();
      setStatsLoading(false);
    };
  }, [id, range.days, range.window, range.from, range.to, platform, selectedLinkId, tick, filtered, tab]);

  // variant breakdown belongs to Analytics: only for a split link selected there
  useEffect(() => {
    const l = (links ?? []).find((x) => x.id === selectedLinkId);
    if (tab !== 'analytics' || !app || !l?.hasSplit || range.window) {
      setVariants(null);
      return;
    }
    const live = variantsLoad.begin();
    api.getLinkVariantStats(app.id, l.id, range).then(
      (v) => live() && setVariants(v),
      () => live() && setVariants([]),
    );
    return variantsLoad.cancel;
  }, [tab, app, selectedLinkId, links, range.days, range.window, range.from, range.to]);

  return {
    app,
    setApp,
    links,
    stats,
    variants,
    error,
    setError,
    statsError,
    statsLoading,
    linkBase,
    tick,
    refreshing,
    refresh,
    load,
    selectedLinkId,
  };
}
