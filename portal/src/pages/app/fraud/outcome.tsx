import { FlaggedInstall } from '../../../lib/types';
import { muted } from '../../../lib/styles';

export function Outcome({ i }: { i: FlaggedInstall }) {
  const text =
    i.fraudAction === 'reattributed'
      ? 'Credit moved to another click'
      : i.fraudAction === 'excluded'
        ? 'Kept as organic'
        : i.attribution === 'non_organic'
          ? 'Attributed, tagged only'
          : 'Organic, tagged only';
  return (
    <div style={{ display: 'grid', gap: 2 }}>
      {text}
      {i.fraudLinkKey && <span style={{ ...muted, fontSize: 12 }}>Flagged click on {i.fraudLinkKey}</span>}
    </div>
  );
}
