import { hostOf } from '../lib/format';
import { CopyButton } from './copy-button';
import { ICONS, IconButton } from './icons';

// no-track flag (server paramNoTrack): portal previews never count as clicks
export const previewUrl = (url: string) => `${url}?detur-no-track=1`;

// full short link: host muted, key strong; copy + open (untracked) beside it
export function ShortLink({ base, linkKey }: { base: string; linkKey: string }) {
  const url = `${base}/${linkKey}`;
  return (
    <span class="short-link">
      <span class="short-link-text" title={url}>
        <span class="short-link-host">{hostOf(base)}/</span>
        <strong>{linkKey}</strong>
      </span>
      {base && (
        <>
          <CopyButton icon value={url} label="Copy link" />
          <IconButton
            icon={ICONS.open}
            label="Open link (not counted as a click)"
            href={previewUrl(url)}
            target="_blank"
            rel="noreferrer"
          />
        </>
      )}
    </span>
  );
}
