import { useState } from 'react';

interface Props {
  submitting: boolean;
  error: string | null;
  onSubmit: (payload: { domains?: string[]; surge_url?: string; surge_inline?: string }) => void;
}

type Mode = 'domains' | 'surge_url' | 'surge_inline';

export default function ProbeForm({ submitting, error, onSubmit }: Props) {
  const [mode, setMode] = useState<Mode>('domains');
  const [text, setText] = useState('');
  const [surgeURL, setSurgeURL] = useState('');

  const submit = () => {
    if (mode === 'domains') {
      const domains = text
        .split(/[\n,\s]+/)
        .map((d) => d.trim())
        .filter(Boolean);
      if (!domains.length) return;
      onSubmit({ domains });
    } else if (mode === 'surge_url') {
      if (!surgeURL.trim()) return;
      onSubmit({ surge_url: surgeURL.trim() });
    } else {
      if (!text.trim()) return;
      onSubmit({ surge_inline: text });
    }
  };

  return (
    <div className="card space-y-3">
      <div className="flex items-center justify-between">
        <h2 className="font-semibold">New Probe</h2>
        <div className="flex gap-1 text-xs">
          {(['domains', 'surge_url', 'surge_inline'] as Mode[]).map((m) => (
            <button
              key={m}
              onClick={() => setMode(m)}
              className={`px-2 py-1 rounded-md border ${
                mode === m ? 'bg-cn2 text-ink-900 border-cn2' : 'border-ink-700 text-slate-300'
              }`}
            >
              {m === 'domains' ? 'Domains' : m === 'surge_url' ? 'Surge URL' : 'Surge Inline'}
            </button>
          ))}
        </div>
      </div>

      {mode === 'surge_url' ? (
        <input
          type="url"
          placeholder="https://example.com/rules.list"
          value={surgeURL}
          onChange={(e) => setSurgeURL(e.target.value)}
          className="w-full bg-ink-900 border border-ink-700 rounded-lg px-3 py-2 text-sm"
        />
      ) : (
        <textarea
          placeholder={
            mode === 'domains'
              ? 'one domain per line: google.com\nnetflix.com'
              : 'paste Surge ruleset contents'
          }
          value={text}
          onChange={(e) => setText(e.target.value)}
          className="w-full h-40 bg-ink-900 border border-ink-700 rounded-lg px-3 py-2 text-sm font-mono"
        />
      )}

      {error && <div className="text-red-400 text-xs">{error}</div>}

      <button className="btn w-full justify-center" onClick={submit} disabled={submitting}>
        {submitting ? 'Submitting…' : 'Start Probe'}
      </button>
    </div>
  );
}
