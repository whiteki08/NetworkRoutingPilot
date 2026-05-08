import { surgeRulesetURL, xrayRoutingURL } from '../api';

export default function DeliveryPanel() {
  return (
    <div className="card space-y-2">
      <h2 className="font-semibold">Delivery</h2>
      <p className="text-xs text-slate-400">
        Subscribe your client to the endpoints below. Responses support <span className="font-mono">ETag</span> and{' '}
        <span className="font-mono">If-None-Match</span> for bandwidth-free polling.
      </p>
      <div className="space-y-2 text-xs font-mono">
        <div className="flex items-center justify-between bg-ink-900 rounded-lg p-2 border border-ink-700">
          <span className="truncate">{surgeRulesetURL()}</span>
          <a className="btn-secondary" href={surgeRulesetURL()} target="_blank" rel="noreferrer">
            Open
          </a>
        </div>
        <div className="flex items-center justify-between bg-ink-900 rounded-lg p-2 border border-ink-700">
          <span className="truncate">{xrayRoutingURL()}</span>
          <a className="btn-secondary" href={xrayRoutingURL()} target="_blank" rel="noreferrer">
            Open
          </a>
        </div>
      </div>
    </div>
  );
}
