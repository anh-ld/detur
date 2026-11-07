import { Badge, Button } from 'kinu';
import { Webhook } from '../../../lib/types';
import { ConfirmDelete } from '../../../components/confirm-delete';
import { CopyButton } from '../../../components/copy-button';
import { mono, muted, row } from '../../../lib/styles';

const mid = { verticalAlign: 'middle' } as const;

const inBackoff = (wh: Webhook) =>
  [wh.backoffInstalls, wh.backoffClicks, wh.backoffEvents].some((b) => b && new Date(b).getTime() > Date.now());

function Status({ wh }: { wh: Webhook }) {
  if (!wh.enabled) return <Badge variant="outline">Paused</Badge>;
  if (inBackoff(wh)) return <Badge variant="destructive">Retrying</Badge>;
  return <Badge>Active</Badge>;
}

export function WebhookRow({
  wh,
  admin,
  onToggle,
  onReplay,
  onRotate,
  onDelete,
}: {
  wh: Webhook;
  admin: boolean;
  onToggle: () => void;
  onReplay: () => void;
  onRotate: () => void;
  onDelete: () => void;
}) {
  const created = new Date(wh.createdAt).toLocaleDateString('en', { month: 'short', day: 'numeric', year: 'numeric' });
  return (
    <tr>
      <td style={mid}>
        <Status wh={wh} />
      </td>
      <td style={mid}>
        <div class="wh-url" title={wh.url}>
          {wh.url}
        </div>
        <div style={{ ...muted, fontSize: 12, marginTop: 2 }}>Created {created}</div>
      </td>
      <td data-label="Events" style={mid}>
        <div style={{ display: 'flex', gap: 6, flexWrap: 'wrap' }}>
          {wh.types.map((t) => (
            <Badge key={t} variant="outline" style={{ fontSize: 11 }}>
              {t}
            </Badge>
          ))}
        </div>
      </td>
      <td data-label="Secret" style={mid}>
        {admin ? (
          <div style={row}>
            <span style={{ ...mono, fontSize: 12 }}>{wh.secret ? wh.secret.slice(0, 10) + '…' : '—'}</span>
            <CopyButton value={wh.secret} label="Copy" />
            <Button size="sm" variant="outline" onClick={onRotate}>
              Rotate
            </Button>
          </div>
        ) : (
          <span style={muted}>Locked</span>
        )}
      </td>
      <td style={{ ...mid, textAlign: 'right' }}>
        <div style={{ ...row, justifyContent: 'flex-end' }}>
          <Button size="sm" variant="outline" onClick={onToggle}>
            {wh.enabled ? 'Pause' : 'Resume'}
          </Button>
          <Button size="sm" variant="outline" onClick={onReplay}>
            Replay
          </Button>
          <ConfirmDelete
            id={`dlg-del-wh-${wh.id}`}
            title="Delete webhook"
            body={`Delete endpoint ${wh.url}? Pending deliveries for this webhook will be discarded.`}
            onConfirm={onDelete}
            requireAdmin
          />
        </div>
      </td>
    </tr>
  );
}
