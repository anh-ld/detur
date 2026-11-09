import { ComponentChildren } from 'preact';
import { useEffect, useRef, useState } from 'preact/hooks';
import { Alert, Tab, TabList } from 'kinu';
import { AdminRequired } from '../../../components/admin';
import { refreshAdminSession, useAdmin } from '../../../lib/admin';
import { muted } from '../../../lib/styles';
import { App } from '../../../lib/types';
import { ApiKeyTable } from './api-key';
import { AppConfigTable } from './app-config';
import { HealthTable } from './health';
import { MatchingTable } from './matching';
import { MatchQualityView } from './match-quality';
import { PageEditor } from './page-editor';
import { RedirectProfilesForm } from './redirect-profiles';
import { SocialPresetsForm } from './social-presets';
import { TagLinksRow } from './tag-links';

const SETTINGS_TABS = [
  ['app', 'App'],
  ['page', 'Page'],
  ['matching', 'Matching'],
] as const;

const sectionTitle = { margin: '40px 0 16px', fontSize: 20 };

function Section({ title, hint, first, children }: { title: string; hint?: string; first?: boolean; children: ComponentChildren }) {
  return (
    <>
      <h2 style={first ? { ...sectionTitle, marginTop: 0 } : sectionTitle}>{title}</h2>
      {hint && <p style={{ ...muted, marginBottom: 16 }}>{hint}</p>}
      {children}
    </>
  );
}

export function SettingsPanel({ app, onApp, onReload }: { app: App; onApp: (a: App) => void; onReload: () => void }) {
  const [tab, setTab] = useState<(typeof SETTINGS_TABS)[number][0]>('app');
  const { locked } = useAdmin();
  useEffect(() => {
    void refreshAdminSession();
  }, []);
  const shown = useRef(false);
  const pageSeen = useRef(false);
  if (tab === 'page') pageSeen.current = true;
  const notice = <AdminRequired>Enter your admin password to view and change settings.</AdminRequired>;
  if (locked && !shown.current) return notice;
  shown.current = true;

  return (
    <div>
      {locked && notice}
      <div hidden={locked}>
        <TabList class="settings-tabs">
          {SETTINGS_TABS.map(([id, label]) => (
            <Tab key={id} role="tab" aria-selected={tab === id} onClick={() => setTab(id)}>
              {label}
            </Tab>
          ))}
        </TabList>

        <div hidden={tab !== 'app'}>
          <Section first title="Health" hint="Setup checks. Refreshes after you save.">
            <HealthTable app={app} />
          </Section>
          <Section title="App config" hint="Used to serve the iOS and Android well-known files.">
            <AppConfigTable app={app} onSaved={onApp} />
          </Section>
          <Section title="Store and web redirect presets">
            <RedirectProfilesForm app={app} onSaved={onApp} />
          </Section>
          <Section title="Social preview presets">
            <SocialPresetsForm app={app} onSaved={onApp} />
          </Section>
          <Section title="API key">
            <ApiKeyTable app={app} onReload={onReload} />
          </Section>
        </div>

        <div hidden={tab !== 'page'}>
          <p style={{ ...muted, marginTop: 0, marginBottom: 16 }}>
            What visitors see before the store: iOS browsers (copy page) and in-app browsers (tap page). Default, or your own HTML.
          </p>
          {pageSeen.current && <PageEditor app={app} />}
        </div>

        <div hidden={tab !== 'matching'}>
          <p style={{ ...muted, marginTop: 0, marginBottom: 16 }}>Applies to every link of this app.</p>
          <MatchingTable app={app} onSaved={onApp} />
          <TagLinksRow app={app} onSaved={onApp} />
          <Section title="Match quality" hint="Installs from the last 30 days: how each was attributed, and the score it got.">
            {tab === 'matching' && <MatchQualityView app={app} />}
          </Section>
        </div>
      </div>
    </div>
  );
}
