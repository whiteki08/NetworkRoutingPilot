import { MapContainer, TileLayer, CircleMarker, Polyline, Tooltip } from 'react-leaflet';
import { STATUS_COLORS } from '../store';
import type { TraceResult } from '../types';

export default function HopMap({ result }: { result: TraceResult | null }) {
  const geoHops = (result?.hops ?? []).filter(
    (h) => typeof h.latitude === 'number' && typeof h.longitude === 'number' && h.latitude !== 0,
  );
  const path: [number, number][] = geoHops.map((h) => [h.latitude!, h.longitude!]);
  const color = result ? STATUS_COLORS[result.classification_status] : '#38bdf8';

  return (
    <div className="card h-[420px] p-0 overflow-hidden relative">
      <div className="absolute top-3 left-3 z-[500] pill bg-ink-900/80 text-slate-200">
        {result ? result.target_domain : 'Select a domain to view its path'}
      </div>
      <MapContainer
        center={[30, 10]}
        zoom={2}
        style={{ height: '100%', width: '100%' }}
        attributionControl={false}
        worldCopyJump
      >
        <TileLayer
          url="https://{s}.basemaps.cartocdn.com/dark_all/{z}/{x}/{y}{r}.png"
          subdomains={['a', 'b', 'c', 'd']}
        />
        {path.length >= 2 && <Polyline positions={path} pathOptions={{ color, weight: 2, opacity: 0.8 }} />}
        {geoHops.map((h, idx) => (
          <CircleMarker
            key={`${h.ip}-${idx}`}
            center={[h.latitude!, h.longitude!]}
            radius={6}
            pathOptions={{ color, fillColor: color, fillOpacity: 0.8 }}
          >
            <Tooltip direction="top" offset={[0, -6]} opacity={1}>
              <div className="text-xs">
                <div className="font-mono">{h.ip}</div>
                <div>
                  TTL {h.ttl} · {h.city ?? ''} {h.country_code ?? ''}
                </div>
                {typeof h.rtt_ms === 'number' && <div>{h.rtt_ms.toFixed(1)} ms</div>}
              </div>
            </Tooltip>
          </CircleMarker>
        ))}
      </MapContainer>
    </div>
  );
}
