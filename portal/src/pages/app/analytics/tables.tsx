import { ComponentChildren } from 'preact';
import { Card, Empty, Table } from 'kinu';
import { Analytics, VariantStat } from '../../../lib/types';
import { fmt } from '../../../lib/format';
import { mono, muted } from '../../../lib/styles';
import { Skeleton } from '../../../components/page';
import { markTitle, rate } from './helpers';

const right = { textAlign: 'right' } as const;

function Section({ title, sub, children }: { title: string; sub: string; children: ComponentChildren }) {
  return (
    <section aria-label={title} style={{ display: 'grid', gap: 12 }}>
      <div style={{ display: 'grid', gap: 4 }}>
        <h3 style={{ margin: 0 }}>{title}</h3>
        <p style={muted}>{sub}</p>
      </div>
      {children}
    </section>
  );
}

// empty in range: same Card + Empty as Links tab
const EmptySection = ({ title, children }: { title: string; children: ComponentChildren }) => (
  <Card>
    <Empty>
      <h3>{title}</h3>
      <p style={{ margin: 0 }}>{children}</p>
    </Empty>
  </Card>
);

export function VariantsSection({ variants }: { variants: VariantStat[] }) {
  return (
    <Section title="Variants" sub="Clicks, installs and conversion per A/B variant">
      {variants.length === 0 ? (
        <Empty>
          <h3>No variant stats yet</h3>
          <p style={muted}>Variant clicks and matched installs appear here after taps.</p>
        </Empty>
      ) : (
        <Table>
          <thead>
            <tr>
              <th>Variant</th>
              <th style={right}>Clicks</th>
              <th style={right}>Installs</th>
              <th style={right}>Conversion</th>
            </tr>
          </thead>
          <tbody>
            {variants.map((v) => (
              <tr key={v.variant}>
                <td>
                  <strong>{v.variant}</strong>
                </td>
                <td data-label="Clicks" className="num">
                  {fmt(v.clicks)}
                </td>
                <td data-label="Installs" className="num">
                  {fmt(v.installs)}
                </td>
                <td data-label="Conversion" className="num">
                  {(v.conversion_rate * 100).toFixed(1)}%
                </td>
              </tr>
            ))}
          </tbody>
        </Table>
      )}
    </Section>
  );
}

export function RetentionSection({ data }: { data: Analytics | null }) {
  const retention = data?.retention ?? []; // older servers / fixtures omit it
  return (
    <Section title="Retention by link" sub="Tagged devices back on day 1, 7 and 30. All platforms.">
      {!data ? (
        <Skeleton h={36} />
      ) : retention.length === 0 ? (
        <EmptySection title="No tagged devices yet">
          Turn on Tag deferred links in Settings, then send <code>detur_link</code> once from the app.
        </EmptySection>
      ) : (
        <Table>
          <thead>
            <tr>
              <th>Link</th>
              <th style={right}>Devices</th>
              <th style={right}>Day 1</th>
              <th style={right}>Day 7</th>
              <th style={right}>Day 30</th>
            </tr>
          </thead>
          <tbody>
            {retention.map((r) => (
              <tr key={r.linkId}>
                <td style={mono}>{r.key}</td>
                <td data-label="Devices" className="num">
                  {fmt(r.devices)}
                </td>
                {(['d1', 'd7', 'd30'] as const).map((m) => (
                  <td key={m} data-label={`Day ${m.slice(1)}`} className="num" title={markTitle(r[m])}>
                    {rate(r[m])}
                  </td>
                ))}
              </tr>
            ))}
          </tbody>
        </Table>
      )}
    </Section>
  );
}

export function ConversionsSection({ data }: { data: Analytics | null }) {
  const conversions = data?.conversions ?? [];
  return (
    <Section title="Conversions by link" sub="Events from tagged devices. All platforms.">
      {!data ? (
        <Skeleton h={36} />
      ) : conversions.length === 0 ? (
        <EmptySection title="No conversions yet">Events from tagged devices show here.</EmptySection>
      ) : (
        <Table>
          <thead>
            <tr>
              <th>Link</th>
              <th>Event</th>
              <th style={right}>Count</th>
            </tr>
          </thead>
          <tbody>
            {conversions.map((c) => (
              <tr key={c.linkId + c.event}>
                <td style={mono}>{c.key}</td>
                <td style={mono}>{c.event}</td>
                <td data-label="Count" className="num">
                  {fmt(c.count)}
                </td>
              </tr>
            ))}
          </tbody>
        </Table>
      )}
    </Section>
  );
}
