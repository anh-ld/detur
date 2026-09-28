import { useEffect, useState } from 'preact/hooks';
import { Button, Input, Label } from 'kinu';
import { api } from './api';

export function SettingsPage() {
  const [threshold, setThreshold] = useState('');
  const [windowMinutes, setWindowMinutes] = useState('');
  const [error, setError] = useState('');
  const [saved, setSaved] = useState(false);

  useEffect(() => {
    api
      .getSettings()
      .then((s) => {
        setThreshold(String(s.threshold));
        setWindowMinutes(String(s.windowMinutes));
      })
      .catch((e) => setError(String(e)));
  }, []);

  const save = async () => {
    setError('');
    setSaved(false);
    const th = Number(threshold);
    const win = Number(windowMinutes);
    if (!Number.isFinite(th) || !Number.isFinite(win)) {
      setError('threshold and window must be numbers');
      return;
    }
    try {
      await api.saveSettings({ threshold: th, windowMinutes: win });
      setSaved(true);
    } catch (e) {
      setError(String(e));
    }
  };

  return (
    <div>
      <div class="page-head">
        <h1>Settings</h1>
      </div>
      <p class="hint">Default matching settings (R14); each link can override these.</p>
      <form
        onSubmit={(e) => {
          e.preventDefault();
          save();
        }}
        style={{ maxWidth: 320 }}
      >
        <Label htmlFor="set-th">Default threshold (700–1200)</Label>
        <Input id="set-th" type="number" value={threshold} onInput={(e) => setThreshold(e.currentTarget.value)} />
        <Label htmlFor="set-win">Default window (minutes, 5–180)</Label>
        <Input
          id="set-win"
          type="number"
          value={windowMinutes}
          onInput={(e) => setWindowMinutes(e.currentTarget.value)}
        />
        {error && <p class="error">{error}</p>}
        {saved && <p class="saved">Saved.</p>}
        <div class="row">
          <Button type="submit">Save</Button>
        </div>
      </form>
    </div>
  );
}