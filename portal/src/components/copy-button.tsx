import { useEffect, useState } from 'preact/hooks';
import { Button } from 'kinu';
import { copyText } from '../lib/clipboard';
import { ICONS, IconButton } from './icons';

// copy button: "Copied" (or a check) for 1.5s. icon = compact ghost version for inline values
export function CopyButton({ value, label, icon }: { value: string; label: string; icon?: boolean }) {
  const [copied, setCopied] = useState(false);
  useEffect(() => {
    if (!copied) return;
    const t = setTimeout(() => setCopied(false), 1500);
    return () => clearTimeout(t);
  }, [copied]);
  const [failed, setFailed] = useState(false);
  const onClick = async () => {
    try {
      await copyText(value);
      setFailed(false);
      setCopied(true);
    } catch {
      setFailed(true); // say so: a silent no-op on the show-once key dialog loses the key
    }
  };
  const text = copied ? 'Copied' : failed ? 'Copy failed: select and copy' : label;
  if (icon) return <IconButton icon={copied ? ICONS.check : ICONS.copy} label={text} onClick={onClick} />;
  return (
    <Button size="sm" variant="outline" onClick={onClick}>
      {text}
    </Button>
  );
}
