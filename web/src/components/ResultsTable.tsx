import { STATUS_COLORS, STATUS_LABELS, useAppStore } from '../store';
import type { TraceResult } from '../types';

export default function ResultsTable({ results }: { results: TraceResult[] }) {
  const { selectedDomain, selectDomain } = useAppStore();
  return (
    <div className="card overflow-hidden">
      <div className="flex items-center justify-between mb-2">
        <h2 className="font-semibold">Trace Results</h2>
        <span className="text-xs text-slate-400">{results.length} domains</span>
      </div>
      <div className="max-h-[420px] overflow-y-auto">
        <table className="w-full text-sm">
          <thead className="text-xs uppercase text-slate-400 sticky top-0 bg-ink-800">
            <tr>
              <th className="text-left py-1">Domain</th>
              <th className="text-left py-1">Status</th>
              <th className="text-right py-1">RTT</th>
              <th className="text-right py-1">Hops</th>
            </tr>
          </thead>
          <tbody>
            {results.map((r) => (
              <tr
                key={r.target_domain}
                onClick={() => selectDomain(r.target_domain)}
                className={`border-t border-ink-700 cursor-pointer hover:bg-ink-700 ${
                  selectedDomain === r.target_domain ? 'bg-ink-700' : ''
                }`}
              >
                <td className="py-1 font-mono text-xs truncate max-w-[220px]">{r.target_domain}</td>
                <td className="py-1">
                  <span
                    className="pill"
                    style={{
                      backgroundColor: STATUS_COLORS[r.classification_status] + '33',
                      color: STATUS_COLORS[r.classification_status],
                    }}
                  >
                    {STATUS_LABELS[r.classification_status]}
                  </span>
                </td>
                <td className="py-1 text-right font-mono text-xs">
                  {r.path_metrics?.destination_rtt_ms ? `${r.path_metrics.destination_rtt_ms.toFixed(1)}ms` : '—'}
                </td>
                <td className="py-1 text-right font-mono text-xs">{r.path_metrics?.total_hops ?? 0}</td>
              </tr>
            ))}
            {!results.length && (
              <tr>
                <td colSpan={4} className="py-6 text-center text-slate-500 text-sm">
                  no results yet
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
    </div>
  );
}
