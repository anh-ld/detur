import { Button } from 'kinu';

// 1.5px outline icon paths (24px grid); decorative, the button label carries the name
export const ICONS = {
  copy: 'M9 9h10a1 1 0 0 1 1 1v10a1 1 0 0 1-1 1H9a1 1 0 0 1-1-1V10a1 1 0 0 1 1-1zM16 9V5a1 1 0 0 0-1-1H5a1 1 0 0 0-1 1v10a1 1 0 0 0 1 1h3',
  check: 'M20 6 9 17l-5-5',
  open: 'M15 3h6v6M10 14 21 3M18 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h6',
  up: 'M12 19V5M5 12l7-7 7 7',
  down: 'M12 5v14M19 12l-7 7-7-7',
  edit: 'M17 3a2.85 2.85 0 0 1 4 4L7.5 20.5 2 22l1.5-5.5Z',
  trash: 'M3 6h18M8 6V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2M19 6l-1 14a2 2 0 0 1-2 2H8a2 2 0 0 1-2-2L5 6',
  close: 'M18 6 6 18M6 6l12 12',
  plus: 'M12 5v14M5 12h14',
};

export const Icon = ({ d, spin, size = 16 }: { d: string; spin?: boolean; size?: number }) => (
  <svg
    class={spin ? 'spin' : undefined}
    width={size}
    height={size}
    viewBox="0 0 24 24"
    fill="none"
    stroke="currentColor"
    stroke-width="1.5"
    stroke-linecap="round"
    stroke-linejoin="round"
    aria-hidden="true"
  >
    <path d={d} />
  </svg>
);

// square ghost button, icon only: label -> aria-label + hover tooltip
export function IconButton({
  icon,
  label,
  spin,
  danger,
  ...rest
}: { icon: string; label: string; spin?: boolean; danger?: boolean } & Record<string, unknown>) {
  return (
    <Button
      type="button"
      size="sm"
      variant="ghost"
      class={danger ? 'icon-btn danger' : 'icon-btn'}
      aria-label={label}
      title={label}
      {...rest}
    >
      <Icon d={icon} spin={spin} />
    </Button>
  );
}
