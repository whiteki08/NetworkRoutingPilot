import { useState } from 'react';
import { lookupRIPE } from '../api';
import type { RIPERecord } from '../types';

export default function RipeLookup() {
  const [resource, setResource] = useState('1.1.1.1');
  const [loading, setLoading] = useState(false);
  const [record, setRecord] = useState<RIPERecord | null>(null);
  const [error, setError] = useState<string | null>(null);

  const run = async () => {
    if (!resource.trim()) return;
    setLoading(true);
    setError(null);
    try {
      setRecord(await lookupRIPE(resource.trim()));
    } catch (e) {
      setError((e as Error).message);
      setRecord(null);
    } finally {
      setLoading(false);
    }
  };

  return (
    <div className="card space-y-3">
      <h2 className="font-semibold">RIPE Toolkit</h2>
      <div className="flex gap-2">
        <input
          value={resource}
          onChange={(e) => setResource(e.target.value)}
          placeholder="IP, prefix, or AS number"
          className="flex-1 bg-ink-900 border border-ink-700 rounded-lg px-3 py-2 text-sm font-mono"
        />
        <button className="btn" onClick={run} disabled={loading}>
          {loading ? '…' : 'Lookup'}
        </button>
      </div>
      {error && <div className="text-red-400 text-xs">{error}</div>}
      {record && (
        <div className="text-xs font-mono bg-ink-900 border border-ink-700 rounded-lg p-3">
          <div>resource: {record.resource}</div>
          {record.target_prefix && <div>prefix: {record.target_prefix}</div>}
          {record.asn ? <div>origin ASN: {record.asn}</div> : null}
          {record.communities?.length ? <div>communities: {record.communities.join(', ')}</div> : null}
          <div className="text-slate-500 mt-1">source: {record.source} · cached: {String(record.cached)}</div>
        </div>
      )}
    </div>
  );
}
