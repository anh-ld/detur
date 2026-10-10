import { cloneElement, JSX } from 'preact';
import { useRef, useState } from 'preact/hooks';
import {
  Alert,
  Badge,
  Button,
  Dialog,
  Empty,
  Field,
  Input,
  Label,
  Select,
  Separator,
  Tab,
  Table,
  TabList,
} from 'kinu';
import { api, Link, LinkRule, Platform, RuleTargets, SplitVariant, VariantStat } from './api';
import { adminCall, closeDialog, Loading, mono, muted, row } from './ui';

const cleanTargets = (t?: RuleTargets): RuleTargets => ({
  destination: t?.destination?.trim() || undefined,
  ios: t?.ios?.trim() || undefined,
  android: t?.android?.trim() || undefined,
  fallback_url: t?.fallback_url?.trim() || undefined,
});

// datetime-local value <-> UTC ISO; the input is read and shown as UTC
const toLocalInput = (iso?: string) => (iso ? iso.slice(0, 16) : '');
const fromLocalInput = (v: string) => (v ? new Date(v + 'Z').toISOString() : undefined);

export function RulesDialog({
  appId,
  link,
  trigger,
  onSaved,
}: {
  appId: string;
  link: Link;
  trigger: JSX.Element;
  onSaved?: () => void;
}) {
  const dialogId = `dlg-rules-${link.id}`;
  const [tab, setTab] = useState<'rules' | 'perf'>('rules');
  const [rules, setRules] = useState<LinkRule[]>([]);
  const [stats, setStats] = useState<VariantStat[]>([]);
  const [loading, setLoading] = useState(false);
  const [saving, setSaving] = useState(false);
  const [err, setErr] = useState('');
  const [success, setSuccess] = useState('');
  const [perfDays, setPerfDays] = useState(30);
  const [dirty, setDirty] = useState(false);
  const reqSeq = useRef(0); // latest load wins; stale responses are dropped

  const [editingIndex, setEditingIndex] = useState<number | null>(null);
  const [draftRule, setDraftRule] = useState<LinkRule | null>(null);
  const [actionType, setActionType] = useState<'direct' | 'split'>('direct');

  const splitVariants = draftRule?.action.split || [];
  const totalWeight = splitVariants.reduce((acc, v) => acc + (Number(v.weight) || 0), 0);
  const isValidWeightSum = totalWeight === 100;

  const updateVariant = (vIdx: number, patch: Partial<SplitVariant>) => {
    const next = [...splitVariants];
    next[vIdx] = { ...next[vIdx], ...patch };
    setDraftRule({ ...draftRule!, action: { ...draftRule!.action, split: next } });
  };

  const loadData = async () => {
    const seq = ++reqSeq.current;
    setLoading(true);
    setErr('');
    try {
      const [r, s] = await Promise.all([
        api.getLinkRules(appId, link.id),
        api.getLinkVariantStats(appId, link.id, perfDays),
      ]);
      if (seq !== reqSeq.current) return;
      setRules(r);
      setStats(s);
      setDirty(false);
    } catch (e) {
      if (seq === reqSeq.current) setErr(String(e));
    } finally {
      if (seq === reqSeq.current) setLoading(false);
    }
  };

  const loadStats = (days: number) => {
    const seq = ++reqSeq.current;
    api
      .getLinkVariantStats(appId, link.id, days)
      .then((s) => seq === reqSeq.current && setStats(s))
      .catch((e) => seq === reqSeq.current && setErr(String(e)));
  };

  const openModal = () => {
    setTab('rules');
    setEditingIndex(null);
    setDraftRule(null);
    setSuccess('');
    loadData();
  };

  const startAddRule = () => {
    setDraftRule({
      name: '',
      cond: {},
      action: {
        targets: {
          destination: '',
          ios: '',
          android: '',
          fallback_url: '',
        },
      },
    });
    setActionType('direct');
    setEditingIndex(-1);
    setErr('');
  };

  const startEditRule = (idx: number) => {
    const r = structuredClone(rules[idx]);
    if (r.action.split && r.action.split.length > 0) {
      setActionType('split');
    } else {
      setActionType('direct');
      if (!r.action.targets) {
        r.action.targets = {};
      }
    }
    setDraftRule(r);
    setEditingIndex(idx);
    setErr('');
  };

  const moveRule = (idx: number, dir: -1 | 1) => {
    const target = idx + dir;
    if (target < 0 || target >= rules.length) return;
    const next = [...rules];
    const [moved] = next.splice(idx, 1);
    next.splice(target, 0, moved);
    setRules(next);
    setDirty(true);
  };

  const deleteRule = (idx: number) => {
    setRules(rules.filter((_, i) => i !== idx));
    setDirty(true);
  };

  const saveDraftRule = () => {
    if (!draftRule) return;
    setErr('');
    const name = draftRule.name.trim();
    if (!name) {
      setErr('Rule name is required');
      return;
    }

    const cleanedRule: LinkRule = {
      ...draftRule,
      name,
      cond: { ...draftRule.cond },
      action: {},
    };

    if (!cleanedRule.cond.platform) delete cleanedRule.cond.platform;
    if (!cleanedRule.cond.from) delete cleanedRule.cond.from;
    if (!cleanedRule.cond.until) delete cleanedRule.cond.until;
    if (cleanedRule.cond.from && cleanedRule.cond.until && new Date(cleanedRule.cond.until) < new Date(cleanedRule.cond.from)) {
      setErr('Active window end must be after start');
      return;
    }
    if (!cleanedRule.cond.lang?.trim()) delete cleanedRule.cond.lang;
    else cleanedRule.cond.lang = cleanedRule.cond.lang.trim();

    if (!cleanedRule.cond.param_key?.trim()) {
      delete cleanedRule.cond.param_key;
      delete cleanedRule.cond.param_val;
    } else {
      cleanedRule.cond.param_key = cleanedRule.cond.param_key.trim();
      if (!cleanedRule.cond.param_val?.trim()) delete cleanedRule.cond.param_val;
      else cleanedRule.cond.param_val = cleanedRule.cond.param_val.trim();
    }

    if (actionType === 'direct') {
      cleanedRule.action.targets = cleanTargets(draftRule.action.targets);
    } else {
      const variants = draftRule.action.split || [];
      if (variants.length < 2) {
        setErr('A/B Split requires at least 2 variants');
        return;
      }
      let weightSum = 0;
      for (const v of variants) {
        if (!v.name.trim()) {
          setErr('All variants must have a name');
          return;
        }
        weightSum += Number(v.weight) || 0;
      }
      if (weightSum !== 100) {
        setErr(`Variant weights must sum to 100% (currently ${weightSum}%)`);
        return;
      }
      cleanedRule.action.split = variants.map((v) => ({
        name: v.name.trim(),
        weight: Number(v.weight),
        targets: cleanTargets(v.targets),
      }));
    }

    const nextRules = [...rules];
    if (editingIndex === -1) {
      nextRules.push(cleanedRule);
    } else if (editingIndex !== null) {
      nextRules[editingIndex] = cleanedRule;
    }

    // variant stats key on the label: direct rule names and split variant names must be unique per link
    const labels = nextRules.flatMap((r) => (r.action.split?.length ? r.action.split.map((v) => v.name) : [r.name]));
    const dup = labels.find((l, i) => labels.indexOf(l) !== i);
    if (dup) {
      setErr(`"${dup}" is already used by another rule or variant on this link`);
      return;
    }

    setRules(nextRules);
    setDirty(true);
    setEditingIndex(null);
    setDraftRule(null);
  };

  const saveAllToBackend = async () => {
    setErr('');
    setSaving(true);
    setSuccess('');
    try {
      const updated = await adminCall(() => api.setLinkRules(appId, link.id, rules));
      setRules(updated);
      setDirty(false);
      setSuccess('Rules saved successfully');
      onSaved?.();
      setTimeout(() => setSuccess(''), 2500);
    } catch (e) {
      setErr(String(e));
    } finally {
      setSaving(false);
    }
  };

  return (
    <Dialog id={dialogId}>
      <Dialog.Trigger>
        {cloneElement(trigger, {
          onClick: openModal,
        })}
      </Dialog.Trigger>
      <Dialog.Content style={{ width: 'min(780px, 95vw)', maxHeight: '88vh', overflowY: 'auto' }}>
        <div style={{ display: 'grid', gap: 16 }}>
          <div style={{ ...row, justifyContent: 'space-between', flexWrap: 'wrap' }}>
            <div style={{ display: 'grid', gap: 4 }}>
              <div style={row}>
                <h2 style={{ margin: 0 }}>Rules & Splits</h2>
                <Badge variant="secondary" style={mono}>
                  /{link.key}
                </Badge>
              </div>
              <p style={muted}>Dynamic routing rules & visitor A/B split testing</p>
            </div>
            <TabList>
              <Tab active={tab === 'rules'} onClick={() => setTab('rules')}>
                Rules ({rules.length})
              </Tab>
              <Tab active={tab === 'perf'} onClick={() => setTab('perf')}>
                Performance
              </Tab>
            </TabList>
          </div>

          {err && (
            <Alert variant="destructive" role="alert">
              {err}
            </Alert>
          )}
          {success && <Alert variant="default">{success}</Alert>}

          {loading && <Loading />}

          {!loading && tab === 'perf' && (
            <div style={{ display: 'grid', gap: 16 }}>
              <div style={{ ...row, justifyContent: 'space-between' }}>
                <span style={muted}>Showing aggregated variant conversions</span>
                <Select
                  value={String(perfDays)}
                  onChange={(e) => {
                    const d = parseInt(e.currentTarget.value) || 30;
                    setPerfDays(d);
                    loadStats(d);
                  }}
                  style={{ width: 140 }}
                >
                  <option value="7">Last 7 days</option>
                  <option value="14">Last 14 days</option>
                  <option value="30">Last 30 days</option>
                  <option value="90">Last 90 days</option>
                </Select>
              </div>

              {stats.length === 0 ? (
                <Empty>
                  <h3>No variant stats yet</h3>
                  <p style={muted}>Variant clicks and matched installs will appear here as visitors tap your link.</p>
                </Empty>
              ) : (
                <Table>
                  <thead>
                    <tr>
                      <th>Variant</th>
                      <th style={{ textAlign: 'right' }}>Clicks</th>
                      <th style={{ textAlign: 'right' }}>Installs</th>
                      <th style={{ textAlign: 'right' }}>Conversion Rate</th>
                    </tr>
                  </thead>
                  <tbody>
                    {stats.map((s, idx) => (
                      <tr key={idx}>
                        <td>
                          <strong>{s.variant}</strong>
                        </td>
                        <td style={{ textAlign: 'right' }}>{s.clicks.toLocaleString()}</td>
                        <td style={{ textAlign: 'right' }}>{s.installs.toLocaleString()}</td>
                        <td style={{ textAlign: 'right' }}>
                          <Badge variant={s.conversion_rate > 0 ? 'default' : 'secondary'}>
                            {(s.conversion_rate * 100).toFixed(1)}%
                          </Badge>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </Table>
              )}
            </div>
          )}

          {!loading && tab === 'rules' && editingIndex !== null && draftRule && (
            <div
              style={{
                display: 'grid',
                gap: 16,
                padding: 16,
                border: '1px solid hsl(var(--k-border))',
                borderRadius: 'var(--k-radius)',
                background: 'hsl(var(--k-card))',
              }}
            >
              <div style={{ ...row, justifyContent: 'space-between' }}>
                <h3 style={{ margin: 0 }}>{editingIndex === -1 ? 'Add New Rule' : `Edit Rule #${editingIndex + 1}`}</h3>
                <Button size="sm" variant="ghost" onClick={() => setEditingIndex(null)}>
                  Cancel
                </Button>
              </div>

              <Field>
                <Label htmlFor={`${dialogId}-rule-name`}>Rule Name</Label>
                <Input
                  id={`${dialogId}-rule-name`}
                  placeholder="e.g. German iOS Campaign"
                  value={draftRule.name}
                  onInput={(e) => setDraftRule({ ...draftRule, name: e.currentTarget.value })}
                />
              </Field>

              <Separator />
              <strong style={{ fontSize: 14 }}>Matching Criteria (AND)</strong>
              <p style={{ ...muted, marginTop: -8 }}>Matches visitor traffic meeting all specified criteria.</p>

              <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(220px, 1fr))', gap: 12 }}>
                <Field>
                  <Label htmlFor={`${dialogId}-platform`}>Platform</Label>
                  <Select
                    id={`${dialogId}-platform`}
                    value={draftRule.cond.platform || ''}
                    onChange={(e) =>
                      setDraftRule({
                        ...draftRule,
                        cond: { ...draftRule.cond, platform: (e.currentTarget.value as Platform) || undefined },
                      })
                    }
                  >
                    <option value="">All Platforms</option>
                    <option value="ios">iOS only</option>
                    <option value="android">Android only</option>
                    <option value="desktop">Desktop only</option>
                  </Select>
                </Field>

                <Field>
                  <Label htmlFor={`${dialogId}-lang`}>Language Prefix</Label>
                  <Input
                    id={`${dialogId}-lang`}
                    placeholder="e.g. de, fr, en-US"
                    value={draftRule.cond.lang || ''}
                    onInput={(e) =>
                      setDraftRule({
                        ...draftRule,
                        cond: { ...draftRule.cond, lang: e.currentTarget.value || undefined },
                      })
                    }
                  />
                </Field>

                <Field>
                  <Label htmlFor={`${dialogId}-param-key`}>Query Param Key</Label>
                  <Input
                    id={`${dialogId}-param-key`}
                    placeholder="e.g. utm_campaign"
                    value={draftRule.cond.param_key || ''}
                    onInput={(e) =>
                      setDraftRule({
                        ...draftRule,
                        cond: { ...draftRule.cond, param_key: e.currentTarget.value || undefined },
                      })
                    }
                  />
                </Field>

                <Field>
                  <Label htmlFor={`${dialogId}-param-val`}>Param Value (optional)</Label>
                  <Input
                    id={`${dialogId}-param-val`}
                    placeholder="e.g. summer"
                    value={draftRule.cond.param_val || ''}
                    onInput={(e) =>
                      setDraftRule({
                        ...draftRule,
                        cond: { ...draftRule.cond, param_val: e.currentTarget.value || undefined },
                      })
                    }
                  />
                </Field>

                <Field>
                  <Label htmlFor={`${dialogId}-from`}>Active From (UTC, optional)</Label>
                  <Input
                    id={`${dialogId}-from`}
                    type="datetime-local"
                    value={toLocalInput(draftRule.cond.from)}
                    onInput={(e) =>
                      setDraftRule({ ...draftRule, cond: { ...draftRule.cond, from: fromLocalInput(e.currentTarget.value) } })
                    }
                  />
                </Field>

                <Field>
                  <Label htmlFor={`${dialogId}-until`}>Active Until (UTC, optional)</Label>
                  <Input
                    id={`${dialogId}-until`}
                    type="datetime-local"
                    value={toLocalInput(draftRule.cond.until)}
                    onInput={(e) =>
                      setDraftRule({ ...draftRule, cond: { ...draftRule.cond, until: fromLocalInput(e.currentTarget.value) } })
                    }
                  />
                </Field>
              </div>

              <Separator />
              <strong style={{ fontSize: 14 }}>Routing Action</strong>

              <div style={row}>
                <Button
                  type="button"
                  size="sm"
                  variant={actionType === 'direct' ? 'default' : 'outline'}
                  onClick={() => setActionType('direct')}
                >
                  Direct Redirect Overrides
                </Button>
                <Button
                  type="button"
                  size="sm"
                  variant={actionType === 'split' ? 'default' : 'outline'}
                  onClick={() => {
                    setActionType('split');
                    if (!draftRule.action.split || draftRule.action.split.length === 0) {
                      setDraftRule({
                        ...draftRule,
                        action: {
                          ...draftRule.action,
                          split: [
                            { name: 'Variant A', weight: 50, targets: {} },
                            { name: 'Variant B', weight: 50, targets: {} },
                          ],
                        },
                      });
                    }
                  }}
                >
                  Weighted A/B Split Test
                </Button>
              </div>

              {actionType === 'direct' ? (
                <div style={{ display: 'grid', gap: 12 }}>
                  <p style={muted}>Leave fields empty to inherit defaults from the parent link.</p>
                  <Field>
                    <Label htmlFor={`${dialogId}-direct-dest`}>In-App Destination Deep Link</Label>
                    <Input
                      id={`${dialogId}-direct-dest`}
                      placeholder="e.g. myapp://promo/special"
                      value={draftRule.action.targets?.destination || ''}
                      onInput={(e) =>
                        setDraftRule({
                          ...draftRule,
                          action: {
                            ...draftRule.action,
                            targets: { ...draftRule.action.targets, destination: e.currentTarget.value },
                          },
                        })
                      }
                    />
                  </Field>
                  <Field>
                    <Label htmlFor={`${dialogId}-direct-ios`}>iOS App Store Target</Label>
                    <Input
                      id={`${dialogId}-direct-ios`}
                      placeholder="e.g. https://apps.apple.com/de/app/id123"
                      value={draftRule.action.targets?.ios || ''}
                      onInput={(e) =>
                        setDraftRule({
                          ...draftRule,
                          action: {
                            ...draftRule.action,
                            targets: { ...draftRule.action.targets, ios: e.currentTarget.value },
                          },
                        })
                      }
                    />
                  </Field>
                  <Field>
                    <Label htmlFor={`${dialogId}-direct-android`}>Android Play Store Target</Label>
                    <Input
                      id={`${dialogId}-direct-android`}
                      placeholder="e.g. https://play.google.com/store/apps/details?id=com.example"
                      value={draftRule.action.targets?.android || ''}
                      onInput={(e) =>
                        setDraftRule({
                          ...draftRule,
                          action: {
                            ...draftRule.action,
                            targets: { ...draftRule.action.targets, android: e.currentTarget.value },
                          },
                        })
                      }
                    />
                  </Field>
                  <Field>
                    <Label htmlFor={`${dialogId}-direct-fallback`}>Desktop / Web Fallback URL</Label>
                    <Input
                      id={`${dialogId}-direct-fallback`}
                      placeholder="e.g. https://example.com/de/landing"
                      value={draftRule.action.targets?.fallback_url || ''}
                      onInput={(e) =>
                        setDraftRule({
                          ...draftRule,
                          action: {
                            ...draftRule.action,
                            targets: { ...draftRule.action.targets, fallback_url: e.currentTarget.value },
                          },
                        })
                      }
                    />
                  </Field>
                </div>
              ) : (
                <div style={{ display: 'grid', gap: 16 }}>
                  <div style={{ ...row, justifyContent: 'space-between' }}>
                    <span style={{ fontSize: 13, color: isValidWeightSum ? 'hsl(var(--k-primary))' : 'hsl(var(--k-destructive))' }}>
                      Total Weight: <strong>{totalWeight}%</strong> {isValidWeightSum ? '✓' : '(Must equal 100%)'}
                    </span>
                    <Button
                      type="button"
                      size="sm"
                      variant="outline"
                      onClick={() => {
                        const newName = `Variant ${String.fromCharCode(65 + splitVariants.length)}`;
                        setDraftRule({
                          ...draftRule,
                          action: {
                            ...draftRule.action,
                            split: [...splitVariants, { name: newName, weight: 0, targets: {} }],
                          },
                        });
                      }}
                    >
                      + Add Variant
                    </Button>
                  </div>

                  {splitVariants.map((v, vIdx) => (
                    <div
                      key={vIdx}
                      style={{
                        display: 'grid',
                        gap: 8,
                        padding: 12,
                        background: 'hsl(var(--k-muted) / 0.3)',
                        border: '1px solid hsl(var(--k-border))',
                        borderRadius: 'var(--k-radius)',
                      }}
                    >
                      <div style={{ ...row, justifyContent: 'space-between' }}>
                        <div style={{ ...row, flex: 1, gap: 12 }}>
                          <Field style={{ flex: 2 }}>
                            <Label htmlFor={`${dialogId}-v-${vIdx}-name`}>Variant Name</Label>
                            <Input
                              id={`${dialogId}-v-${vIdx}-name`}
                              value={v.name}
                              onInput={(e) => updateVariant(vIdx, { name: e.currentTarget.value })}
                            />
                          </Field>
                          <Field style={{ width: 100 }}>
                            <Label htmlFor={`${dialogId}-v-${vIdx}-weight`}>Weight %</Label>
                            <Input
                              id={`${dialogId}-v-${vIdx}-weight`}
                              type="number"
                              min="1"
                              max="100"
                              value={v.weight}
                              onInput={(e) => updateVariant(vIdx, { weight: parseInt(e.currentTarget.value) || 0 })}
                            />
                          </Field>
                        </div>
                        {splitVariants.length > 2 && (
                          <Button
                            type="button"
                            size="sm"
                            variant="ghost"
                            style={{ color: 'hsl(var(--k-destructive))', marginTop: 18 }}
                            onClick={() => {
                              const next = splitVariants.filter((_, idx) => idx !== vIdx);
                              setDraftRule({ ...draftRule, action: { ...draftRule.action, split: next } });
                            }}
                          >
                            Remove
                          </Button>
                        )}
                      </div>

                      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(200px, 1fr))', gap: 8 }}>
                        <Input
                          placeholder="In-App Deep Link Override"
                          value={v.targets?.destination || ''}
                          onInput={(e) => updateVariant(vIdx, { targets: { ...v.targets, destination: e.currentTarget.value } })}
                        />
                        <Input
                          placeholder="Desktop Fallback Override"
                          value={v.targets?.fallback_url || ''}
                          onInput={(e) => updateVariant(vIdx, { targets: { ...v.targets, fallback_url: e.currentTarget.value } })}
                        />
                      </div>
                    </div>
                  ))}
                </div>
              )}

              <div style={{ ...row, justifyContent: 'flex-end', gap: 8, marginTop: 8 }}>
                <Button variant="outline" onClick={() => setEditingIndex(null)}>
                  Cancel
                </Button>
                <Button onClick={saveDraftRule}>Done Editing</Button>
              </div>
            </div>
          )}

          {!loading && tab === 'rules' && (editingIndex === null || !draftRule) && (
            <div style={{ display: 'grid', gap: 16 }}>
              {rules.length === 0 ? (
                <Empty>
                  <h3>No rules configured</h3>
                  <p style={muted}>This link routes directly to its base targets without rules or split testing.</p>
                  <Button disabled={saving} onClick={startAddRule}>
                    + Add First Rule
                  </Button>
                </Empty>
              ) : (
                <div style={{ display: 'grid', gap: 8 }}>
                  {rules.map((r, idx) => {
                    const isSplit = r.action.split && r.action.split.length > 0;
                    return (
                      <div
                        key={r.id || idx}
                        style={{
                          ...row,
                          justifyContent: 'space-between',
                          padding: '12px 16px',
                          border: '1px solid hsl(var(--k-border))',
                          borderRadius: 'var(--k-radius)',
                          background: 'hsl(var(--k-card))',
                        }}
                      >
                        <div style={{ display: 'grid', gap: 4 }}>
                          <div style={row}>
                            <Badge variant="outline">#{idx + 1}</Badge>
                            <strong>{r.name}</strong>
                            {isSplit ? (
                              <Badge variant="default">A/B Split ({r.action.split?.length} variants)</Badge>
                            ) : (
                              <Badge variant="secondary">Direct Target</Badge>
                            )}
                          </div>
                          <div style={{ ...row, flexWrap: 'wrap', gap: 6, fontSize: 13 }}>
                            {r.cond.platform && <Badge variant="outline">{r.cond.platform}</Badge>}
                            {r.cond.lang && <Badge variant="outline">Lang: {r.cond.lang}</Badge>}
                            {r.cond.param_key && (
                              <Badge variant="outline">
                                ?{r.cond.param_key}
                                {r.cond.param_val ? `=${r.cond.param_val}` : ''}
                              </Badge>
                            )}
                            {(r.cond.from || r.cond.until) && (
                              <Badge variant="outline">
                                {toLocalInput(r.cond.from) || '…'} → {toLocalInput(r.cond.until) || '…'} UTC
                              </Badge>
                            )}
                            {!r.cond.platform && !r.cond.lang && !r.cond.param_key && !r.cond.from && !r.cond.until && (
                              <span style={muted}>Always matches (Fallback)</span>
                            )}
                          </div>
                        </div>

                        <div style={row}>
                          <Button
                            size="sm"
                            variant="ghost"
                            disabled={saving || idx === 0}
                            onClick={() => moveRule(idx, -1)}
                            title="Move Up"
                          >
                            ↑
                          </Button>
                          <Button
                            size="sm"
                            variant="ghost"
                            disabled={saving || idx === rules.length - 1}
                            onClick={() => moveRule(idx, 1)}
                            title="Move Down"
                          >
                            ↓
                          </Button>
                          <Button size="sm" variant="outline" disabled={saving} onClick={() => startEditRule(idx)}>
                            Edit
                          </Button>
                          <Button
                            size="sm"
                            variant="ghost"
                            style={{ color: 'hsl(var(--k-destructive))' }}
                            disabled={saving}
                            onClick={() => deleteRule(idx)}
                          >
                            Delete
                          </Button>
                        </div>
                      </div>
                    );
                  })}
                </div>
              )}
              {(rules.length > 0 || dirty) && (
                <div style={{ ...row, justifyContent: 'space-between', marginTop: 8 }}>
                  {rules.length > 0 ? (
                    <Button variant="outline" disabled={saving} onClick={startAddRule}>
                      + Add Rule
                    </Button>
                  ) : (
                    <span />
                  )}
                  <Button disabled={saving} aria-busy={saving} onClick={saveAllToBackend}>
                    {saving ? 'Saving...' : 'Save Rule Order & Changes'}
                  </Button>
                </div>
              )}
            </div>
          )}

          <div style={{ ...row, justifyContent: 'flex-end', marginTop: 8 }}>
            <Button variant="outline" disabled={saving} onClick={() => closeDialog(dialogId)}>
              Close
            </Button>
          </div>
        </div>
      </Dialog.Content>
    </Dialog>
  );
}
