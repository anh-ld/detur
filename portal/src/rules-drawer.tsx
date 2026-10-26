import { cloneElement, JSX } from 'preact';
import { useEffect, useRef, useState } from 'preact/hooks';
import { Alert, Button, Dialog, Empty, Field, Input, Label, Select, Tab, Table, TabList, Toggle, ToggleGroup } from 'kinu';
import { api, Link, LinkRule, Platform, RuleTargets, SplitVariant, VariantStat } from './api';
import { adminCall, closeDialog, Icon, IconButton, ICONS, Loading, muted, row, ShortLink } from './ui';

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
  linkBase = '',
  trigger,
  onSaved,
  days = 30,
}: {
  appId: string;
  link: Link;
  linkBase?: string;
  trigger: JSX.Element;
  onSaved?: () => void;
  days?: number; // page date filter: Performance follows it
}) {
  const dialogId = `dlg-rules-${link.id}`;
  const [tab, setTab] = useState<'rules' | 'perf'>('rules');
  const [rules, setRules] = useState<LinkRule[]>([]);
  const [stats, setStats] = useState<VariantStat[]>([]);
  const [loading, setLoading] = useState(false);
  const [saving, setSaving] = useState(false);
  const [err, setErr] = useState('');
  const [success, setSuccess] = useState('');
  const perfDays = days;
  const [dirty, setDirty] = useState(false);
  const reqSeq = useRef(0); // latest load wins, stale dropped

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

  // close (Escape/backdrop/button) keeps unsaved edits; reopen shows them until Save/Discard
  const discardChanges = () => {
    setEditingIndex(null);
    setDraftRule(null);
    setSuccess('');
    loadData();
  };
  const openModal = () => {
    setTab('rules');
    if (dirty || draftRule) return;
    discardChanges();
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
        const w = Number(v.weight);
        if (!Number.isInteger(w) || w < 1 || w > 100) {
          setErr(`Variant "${v.name.trim()}" weight must be 1–100%`);
          return;
        }
        weightSum += w;
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

    // variant stats key on label: rule + split variant names unique per link
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
      setSuccess('Rules saved');
      onSaved?.();
      setTimeout(() => setSuccess(''), 2500);
    } catch (e) {
      setErr(String(e));
    } finally {
      setSaving(false);
    }
  };

  const setCond = (patch: Partial<LinkRule['cond']>) => setDraftRule({ ...draftRule!, cond: { ...draftRule!.cond, ...patch } });
  const setTarget = (patch: RuleTargets) =>
    setDraftRule({ ...draftRule!, action: { ...draftRule!.action, targets: { ...draftRule!.action.targets, ...patch } } });
  const editing = tab === 'rules' && editingIndex !== null && !!draftRule;
  const fid = (s: string) => `${dialogId}-${s}`;

  // leaving the editor (Done/Cancel): focus the next step, Save if enabled else Close.
  // Without this, focus stays on whatever button took the clicked one's place (e.g. Discard).
  const wasEditing = useRef(false);
  useEffect(() => {
    if (wasEditing.current && !editing) {
      const save = document.getElementById(fid('save')) as HTMLButtonElement | null;
      (save && !save.disabled ? save : document.getElementById(fid('close')))?.focus();
    }
    wasEditing.current = editing;
  }, [editing]);

  return (
    <Dialog id={dialogId}>
      <Dialog.Trigger>
        {cloneElement(trigger, {
          onClick: openModal,
        })}
      </Dialog.Trigger>
      <Dialog.Content style={{ width: 'min(760px, 95vw)', maxHeight: '88vh', overflowY: 'auto' }}>
        <div style={{ display: 'grid', gap: 20 }}>
          <div style={{ ...row, justifyContent: 'space-between', alignItems: 'flex-start', flexWrap: 'wrap', gap: 12 }}>
            <div style={{ display: 'grid', gap: 4 }}>
              <h2 style={{ margin: 0 }}>Rules & splits</h2>
              <ShortLink base={linkBase} linkKey={link.key} />
            </div>
            {!editing && (
              <TabList>
                <Tab role="tab" aria-selected={tab === 'rules'} onClick={() => setTab('rules')}>
                  Rules ({rules.length})
                </Tab>
                <Tab role="tab" aria-selected={tab === 'perf'} onClick={() => setTab('perf')}>
                  Performance
                </Tab>
              </TabList>
            )}
          </div>

          {err && (
            <Alert variant="destructive" role="alert">
              {err}
            </Alert>
          )}
          {success && <Alert variant="default">{success}</Alert>}

          {loading && <Loading />}

          {!loading && tab === 'perf' && (
            <div style={{ display: 'grid', gap: 12 }}>
              <span style={muted}>Clicks and installs per rule and variant, last {perfDays} days.</span>
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
                      <th style={{ textAlign: 'right' }}>Conversion</th>
                    </tr>
                  </thead>
                  <tbody>
                    {stats.map((s, idx) => (
                      <tr key={idx}>
                        <td>
                          <strong>{s.variant}</strong>
                        </td>
                        <td data-label="Clicks" className="num">{s.clicks.toLocaleString()}</td>
                        <td data-label="Installs" className="num">{s.installs.toLocaleString()}</td>
                        <td data-label="Conversion" className="num">{(s.conversion_rate * 100).toFixed(1)}%</td>
                      </tr>
                    ))}
                  </tbody>
                </Table>
              )}
            </div>
          )}

          {!loading && editing && draftRule && (
            <div class="rule-editor">
              <h3>{editingIndex === -1 ? 'New rule' : `Edit rule ${editingIndex! + 1}`}</h3>
              <Field>
                <Label htmlFor={fid('rule-name')}>Rule name</Label>
                <Input
                  id={fid('rule-name')}
                  placeholder="e.g. German iOS campaign"
                  value={draftRule.name}
                  onInput={(e) => setDraftRule({ ...draftRule, name: e.currentTarget.value })}
                />
              </Field>

              <section class="form-section">
                <h4>When</h4>
                <p style={muted}>Every filled condition must match. Leave all empty to match everyone.</p>
                <div class="form-grid">
                  <Field>
                    <Label htmlFor={fid('platform')}>Platform</Label>
                    <Select
                      id={fid('platform')}
                      value={draftRule.cond.platform || ''}
                      onChange={(e) => setCond({ platform: (e.currentTarget.value as Platform) || undefined })}
                    >
                      <option value="">Any platform</option>
                      <option value="ios">iOS</option>
                      <option value="android">Android</option>
                      <option value="desktop">Desktop</option>
                    </Select>
                  </Field>
                  <Field>
                    <Label htmlFor={fid('lang')}>Language starts with</Label>
                    <Input
                      id={fid('lang')}
                      placeholder="e.g. de, fr, en-US"
                      value={draftRule.cond.lang || ''}
                      onInput={(e) => setCond({ lang: e.currentTarget.value || undefined })}
                    />
                  </Field>
                  <Field>
                    <Label htmlFor={fid('param-key')}>Query param</Label>
                    <Input
                      id={fid('param-key')}
                      placeholder="e.g. utm_campaign"
                      value={draftRule.cond.param_key || ''}
                      onInput={(e) => setCond({ param_key: e.currentTarget.value || undefined })}
                    />
                  </Field>
                  <Field>
                    <Label htmlFor={fid('param-val')}>Equals</Label>
                    <Input
                      id={fid('param-val')}
                      placeholder="Any value"
                      disabled={!draftRule.cond.param_key}
                      value={draftRule.cond.param_val || ''}
                      onInput={(e) => setCond({ param_val: e.currentTarget.value || undefined })}
                    />
                  </Field>
                  <Field>
                    <Label htmlFor={fid('from')}>Active from (UTC)</Label>
                    <Input
                      id={fid('from')}
                      type="datetime-local"
                      value={toLocalInput(draftRule.cond.from)}
                      onInput={(e) => setCond({ from: fromLocalInput(e.currentTarget.value) })}
                    />
                  </Field>
                  <Field>
                    <Label htmlFor={fid('until')}>Active until (UTC)</Label>
                    <Input
                      id={fid('until')}
                      type="datetime-local"
                      value={toLocalInput(draftRule.cond.until)}
                      onInput={(e) => setCond({ until: fromLocalInput(e.currentTarget.value) })}
                    />
                  </Field>
                </div>
              </section>

              <section class="form-section">
                <div style={{ ...row, justifyContent: 'space-between', flexWrap: 'wrap', gap: 12 }}>
                  <h4>Then</h4>
                  <ToggleGroup role="group" aria-label="Action" class="seg">
                    <Toggle type="button" size="sm" pressed={actionType === 'direct'} onClickCapture={() => {}} onClick={() => setActionType('direct')}>
                      Send to
                    </Toggle>
                    <Toggle
                      type="button"
                      size="sm"
                      pressed={actionType === 'split'}
                      onClickCapture={() => {}}
                      onClick={() => {
                        setActionType('split');
                        if (!draftRule.action.split?.length) {
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
                      A/B split
                    </Toggle>
                  </ToggleGroup>
                </div>

                {actionType === 'direct' ? (
                  <>
                    <p style={muted}>Empty fields keep the link's own value, shown faded.</p>
                    <TargetFields
                      idp={fid('direct')}
                      t={draftRule.action.targets}
                      link={link}
                      onChange={setTarget}
                    />
                  </>
                ) : (
                  <div style={{ display: 'grid', gap: 12 }}>
                    <SplitBar variants={splitVariants} />
                    <span class={isValidWeightSum ? 'weight-ok' : 'weight-bad'} role="status">
                      {isValidWeightSum
                        ? 'Weights add up to 100%'
                        : totalWeight < 100
                          ? `${100 - totalWeight}% unassigned, weights must add up to 100%`
                          : `${totalWeight - 100}% over, weights must add up to 100%`}
                    </span>
                    <p style={muted}>Each variant's empty fields keep the link's own value, shown faded.</p>
                    {splitVariants.map((v, vIdx) => (
                      <div key={vIdx} class="variant">
                        <div class="variant-head">
                          <span class="variant-dot" style={{ background: variantColor(vIdx) }} aria-hidden="true" />
                          <Field style={{ flex: 1 }}>
                            <Label htmlFor={fid(`v-${vIdx}-name`)}>Variant name</Label>
                            <Input
                              id={fid(`v-${vIdx}-name`)}
                              value={v.name}
                              onInput={(e) => updateVariant(vIdx, { name: e.currentTarget.value })}
                            />
                          </Field>
                          <Field style={{ width: 110 }}>
                            <Label htmlFor={fid(`v-${vIdx}-weight`)}>Weight</Label>
                            <div class="suffix-input">
                              <Input
                                id={fid(`v-${vIdx}-weight`)}
                                type="number"
                                min="1"
                                max="100"
                                value={v.weight}
                                onInput={(e) => updateVariant(vIdx, { weight: parseInt(e.currentTarget.value) || 0 })}
                              />
                              <span aria-hidden="true">%</span>
                            </div>
                          </Field>
                          {splitVariants.length > 2 && (
                            <IconButton
                              icon={ICONS.close}
                              label={`Remove ${v.name || 'variant'}`}
                              onClick={() =>
                                setDraftRule({
                                  ...draftRule,
                                  action: { ...draftRule.action, split: splitVariants.filter((_, i) => i !== vIdx) },
                                })
                              }
                            />
                          )}
                        </div>
                        <TargetFields
                          idp={fid(`v-${vIdx}`)}
                          t={v.targets}
                          link={link}
                          onChange={(patch) => updateVariant(vIdx, { targets: { ...v.targets, ...patch } })}
                        />
                      </div>
                    ))}
                    <Button
                      type="button"
                      size="sm"
                      variant="ghost"
                      style={{ justifySelf: 'start' }}
                      onClick={() =>
                        setDraftRule({
                          ...draftRule,
                          action: {
                            ...draftRule.action,
                            split: [
                              ...splitVariants,
                              {
                                name: `Variant ${String.fromCharCode(65 + splitVariants.length)}`,
                                weight: Math.max(0, 100 - totalWeight),
                                targets: {},
                              },
                            ],
                          },
                        })
                      }
                    >
                      <Icon d={ICONS.plus} />
                      Add variant
                    </Button>
                  </div>
                )}
              </section>
            </div>
          )}

          {!loading && tab === 'rules' && !editing && (
            rules.length === 0 ? (
              <Empty>
                <h3>No rules yet</h3>
                <p style={muted}>Every visitor gets the link's own targets. Add a rule to route by platform, language, query or date, or to run an A/B split.</p>
                <Button disabled={saving} onClick={startAddRule}>
                  Add rule
                </Button>
              </Empty>
            ) : (
              <div style={{ display: 'grid', gap: 8 }}>
                <span style={muted}>Checked top to bottom. The first rule that matches wins.</span>
                <ol class="rule-list">
                  {rules.map((r, idx) => {
                    const conds = condLabels(r.cond);
                    return (
                      <li key={r.id || idx} class="rule-row">
                        <span class="rule-num">{idx + 1}</span>
                        <div class="rule-body">
                          <strong>{r.name}</strong>
                          <div class="rule-line">
                            <span class="rule-kw">If</span>
                            {conds.length ? conds.map((c) => <span key={c} class="cond">{c}</span>) : <span style={muted}>any visitor</span>}
                          </div>
                          <div class="rule-line">
                            <span class="rule-kw">Then</span>
                            {r.action.split?.length ? (
                              <SplitBar variants={r.action.split} compact />
                            ) : (
                              <TargetSummary t={r.action.targets} />
                            )}
                          </div>
                        </div>
                        <div class="rule-actions">
                          <IconButton icon={ICONS.up} label="Move up" disabled={saving || idx === 0} onClick={() => moveRule(idx, -1)} />
                          <IconButton icon={ICONS.down} label="Move down" disabled={saving || idx === rules.length - 1} onClick={() => moveRule(idx, 1)} />
                          <IconButton icon={ICONS.edit} label="Edit" disabled={saving} onClick={() => startEditRule(idx)} />
                          <IconButton icon={ICONS.trash} label="Delete" class="icon-btn danger" disabled={saving} onClick={() => deleteRule(idx)} />
                        </div>
                      </li>
                    );
                  })}
                  <li class="rule-row rule-default">
                    <span class="rule-num" aria-hidden="true" />
                    <div class="rule-body">
                      <div class="rule-line">
                        <span class="rule-kw">Else</span>
                        <span style={muted}>link default</span>
                        <code class="target">{link.url}</code>
                      </div>
                    </div>
                  </li>
                </ol>
              </div>
            )
          )}

          <div class="dialog-actions">
            {editing ? (
              <>
                <Button variant="outline" onClick={() => setEditingIndex(null)}>
                  Cancel
                </Button>
                <Button onClick={saveDraftRule}>Done</Button>
              </>
            ) : (
              <>
                {tab === 'rules' && rules.length > 0 && !loading && (
                  <Button variant="outline" disabled={saving} onClick={startAddRule} style={{ marginInlineEnd: 'auto' }}>
                    <Icon d={ICONS.plus} />
                    Add rule
                  </Button>
                )}
                {dirty && !saving && (
                  <Button variant="ghost" onClick={discardChanges}>
                    Discard
                  </Button>
                )}
                <Button id={fid('close')} variant="outline" disabled={saving} onClick={() => closeDialog(dialogId)}>
                  Close
                </Button>
                {tab === 'rules' && (rules.length > 0 || dirty) && (
                  <Button id={fid('save')} disabled={saving || !dirty} aria-busy={saving} onClick={saveAllToBackend}>
                    {saving ? 'Saving…' : 'Save rules'}
                  </Button>
                )}
              </>
            )}
          </div>
        </div>
      </Dialog.Content>
    </Dialog>
  );
}

const VARIANT_COLORS = ['205 87% 59%', '205 90% 34%', '187 72% 42%', '35 92% 55%', '262 55% 62%', '340 70% 60%'];
const variantColor = (i: number) => `hsl(${VARIANT_COLORS[i % VARIANT_COLORS.length]})`;

const PLATFORM_LABEL: Record<string, string> = { ios: 'iOS', android: 'Android', desktop: 'Desktop' };
const fmtUtc = (iso: string) => toLocalInput(iso).replace('T', ' ');

// rule conditions as short chips
function condLabels(c: LinkRule['cond']): string[] {
  const out: string[] = [];
  if (c.platform) out.push(PLATFORM_LABEL[c.platform] ?? c.platform);
  if (c.lang) out.push(`language ${c.lang}*`);
  if (c.param_key) out.push(c.param_val ? `?${c.param_key}=${c.param_val}` : `has ?${c.param_key}`);
  if (c.from && c.until) out.push(`${fmtUtc(c.from)} → ${fmtUtc(c.until)} UTC`);
  else if (c.from) out.push(`from ${fmtUtc(c.from)} UTC`);
  else if (c.until) out.push(`until ${fmtUtc(c.until)} UTC`);
  return out;
}

// what a direct rule overrides; nothing set = link default
function TargetSummary({ t }: { t?: RuleTargets }) {
  const parts = [
    t?.destination && <code class="target">{t.destination}</code>,
    t?.ios && <span class="cond">iOS store</span>,
    t?.android && <span class="cond">Android store</span>,
    t?.fallback_url && <span class="cond">desktop page</span>,
  ].filter(Boolean);
  return parts.length ? <>{parts}</> : <span style={muted}>link default</span>;
}

// stacked weight bar + legend. compact = list row (name + % only)
function SplitBar({ variants, compact }: { variants: SplitVariant[]; compact?: boolean }) {
  return (
    <div class={compact ? 'split split-compact' : 'split'}>
      <div class="split-bar" aria-hidden="true">
        {variants.map((v, i) => (
          <span key={i} style={{ flexGrow: Math.max(0, Number(v.weight) || 0), background: variantColor(i) }} />
        ))}
      </div>
      <div class="split-legend">
        {variants.map((v, i) => (
          <span key={i}>
            <i style={{ background: variantColor(i) }} />
            {v.name || 'Unnamed'} <b>{Number(v.weight) || 0}%</b>
          </span>
        ))}
      </div>
    </div>
  );
}

// 4 override fields; placeholders show what an empty field falls back to
function TargetFields({
  idp,
  t,
  link,
  onChange,
}: {
  idp: string;
  t?: RuleTargets;
  link: Link;
  onChange: (patch: RuleTargets) => void;
}) {
  const fields: { k: keyof RuleTargets; label: string; base: string; hint: string }[] = [
    { k: 'destination', label: 'App route', base: link.url, hint: '/promo/special' },
    { k: 'fallback_url', label: 'Desktop fallback', base: link.fallbackUrl, hint: 'https://example.com/landing' },
    { k: 'ios', label: 'iOS store page', base: link.ios, hint: 'https://apps.apple.com/app/id123' },
    { k: 'android', label: 'Android store page', base: link.android, hint: 'https://play.google.com/store/apps/details?id=…' },
  ];
  return (
    <div class="form-grid inherit-fields">
      {fields.map((f) => (
        <Field key={f.k}>
          <Label htmlFor={`${idp}-${f.k}`}>{f.label}</Label>
          <Input
            id={`${idp}-${f.k}`}
            placeholder={f.base || f.hint}
            value={t?.[f.k] || ''}
            onInput={(e) => onChange({ [f.k]: e.currentTarget.value })}
          />
        </Field>
      ))}
    </div>
  );
}
