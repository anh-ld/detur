import { Icon } from './icons';
import { setTz, useTz } from '../lib/tz';

export function TzToggle() {
  const local = useTz() === 'local';
  return (
    <button
      type="button"
      class="nav-link"
      aria-label="Time zone"
      title={local ? 'Showing browser time. Click for UTC' : 'Showing UTC. Click for browser time'}
      onClick={() => setTz(local ? '0' : 'local')}
    >
      <Icon d="M12 22a10 10 0 1 0 0-20 10 10 0 0 0 0 20zM12 6v6l4 2" />
      {local ? 'Local' : 'UTC'}
    </button>
  );
}
