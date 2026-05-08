import ReactECharts from 'echarts-for-react';
import { STATUS_COLORS, STATUS_LABELS } from '../store';
import type { ClassificationStatus } from '../types';

export default function ClassificationChart({ counts }: { counts: Record<string, number> }) {
  const entries = (Object.keys(STATUS_LABELS) as ClassificationStatus[])
    .map((key) => ({
      name: STATUS_LABELS[key],
      value: counts[key] ?? 0,
      itemStyle: { color: STATUS_COLORS[key] },
    }))
    .filter((e) => e.value > 0);

  const option = {
    tooltip: { trigger: 'item' },
    legend: { bottom: 0, textStyle: { color: '#cbd5e1' } },
    series: [
      {
        type: 'pie',
        radius: ['45%', '70%'],
        avoidLabelOverlap: true,
        itemStyle: { borderColor: '#0b1220', borderWidth: 2 },
        label: { color: '#e2e8f0' },
        data: entries.length ? entries : [{ name: 'No data', value: 1, itemStyle: { color: '#334155' } }],
      },
    ],
  };

  return (
    <div className="card">
      <h2 className="font-semibold mb-2">Classification Mix</h2>
      <ReactECharts option={option} style={{ height: 240 }} theme="dark" />
    </div>
  );
}
