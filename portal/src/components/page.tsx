import { ComponentChildren } from 'preact';
import { Skeleton as KSkeleton, Spinner } from 'kinu';
import { muted, row } from '../lib/styles';

// kinu Spinner is inline-block -> center it
export const Loading = () => (
  <div style={{ display: 'grid', placeItems: 'center', padding: '48px 0' }}>
    <Spinner />
  </div>
);

export function PageHeader({
  title,
  description,
  actions,
}: {
  title: ComponentChildren;
  description?: ComponentChildren;
  actions?: ComponentChildren;
}) {
  return (
    <div style={{ ...row, justifyContent: 'space-between', margin: '32px 0 24px', flexWrap: 'wrap', gap: 16 }}>
      <div style={{ display: 'grid', gap: 6 }}>
        <h1 style={{ margin: 0, fontSize: 28 }}>{title}</h1>
        {description && <p style={muted}>{description}</p>}
      </div>
      {actions && (
        <div class="page-actions" style={{ ...row, flexWrap: 'wrap' }}>
          {actions}
        </div>
      )}
    </div>
  );
}

export const Skeleton = ({ w = '100%', h }: { w?: number | string; h: number }) => (
  <KSkeleton style={{ width: w, height: h }} />
);
