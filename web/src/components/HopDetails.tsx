import type { TraceResult } from '../types';
import { STATUS_COLORS, STATUS_LABELS } from '../store';

export default function HopDetails({ result }: { result: TraceResult | null }) {
  if (!result) {
    return (
      <div className="card">
        <h2 className="font-semibold">Hop Details</h2>
        <p className="text-slate-400 text-sm mt-2">Pick a domain from the table to inspect its hops.</p>
      </div>
    );
  }
  return (
    <div className="card space-y-2">
      <div className="flex items-center justify-between">
        <h2 className="font-semibold truncate">{result.target_domain}</h2>
        <span
          className="pill"
          style={{
            backgroundColor: STATUS_COLORS[result.classification_status] + '33',
            color: STATUS_COLORS[result.classification_status],
          }}
        >
          {STATUS_LABELS[result.classification_status]}
        </span>
      </div>
      <div className="text-xs text-slate-400 font-mono">
        resolved {result.resolved_ip_v4 || '—'} · dest RTT{' '}
        {result.path_metrics?.destination_rtt_ms?.toFixed(1) ?? '—'}ms
        {result.path_metrics?.border_delta_ms !== undefined &&
          ` · border Δ ${result.path_metrics.border_delta_ms.toFixed(2)}ms`}
      </div>
      <div className="text-xs text-slate-400 font-mono">
        ASN path: {result.path_metrics?.asn_sequence?.join(' → ') || '—'}
      </div>
      <div className="max-h-[260px] overflow-y-auto mt-2">
        <table className="w-full text-xs font-mono">
          <thead className="text-slate-400 sticky top-0 bg-ink-800">
            <tr>
              <th className="text-left py-1">TTL</th>
              <th className="text-left py-1">IP</th>
              <th className="text-left py-1">Loc</th>
              <th className="text-right py-1">ASN</th>
              <th className="text-right py-1">RTT</th>
            </tr>
          </thead>
          <tbody>
            {result.hops.map((h, idx) => (
              <tr key={idx} className="border-t border-ink-700">
                <td className="py-1">{h.ttl}</td>
                <td className="py-1">{h.ip || (h.responded ? '?' : '*')}</td>
                <td className="py-1">
                  {h.city ? `${h.city}, ` : ''}
                  {h.country_code ?? ''}
                </td>
                <td className="py-1 text-right">{h.asn ?? '—'}</td>
                <td className="py-1 text-right">{typeof h.rtt_ms === 'number' ? h.rtt_ms.toFixed(1) : '—'}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      {result.error && <div className="text-xs text-red-400">{result.error}</div>}
    </div>
  );
}
