import type { ProbeJob } from '../types';

export default function JobProgress({ job }: { job: ProbeJob | null }) {
  if (!job) {
    return (
      <div className="card">
        <h2 className="font-semibold">Job Status</h2>
        <p className="text-slate-400 text-sm mt-2">No active job. Submit a probe to get started.</p>
      </div>
    );
  }
  const pct = job.total > 0 ? Math.round((job.processed / job.total) * 100) : 0;
  return (
    <div className="card space-y-2">
      <div className="flex items-center justify-between">
        <h2 className="font-semibold">Job {job.id.slice(0, 8)}</h2>
        <span className="pill bg-ink-700 text-slate-200 capitalize">{job.status}</span>
      </div>
      <div className="text-xs text-slate-400">
        {job.processed}/{job.total} domains processed
      </div>
      <div className="w-full h-2 bg-ink-900 rounded-full overflow-hidden">
        <div className="h-full bg-cn2 transition-all" style={{ width: `${pct}%` }} />
      </div>
      {job.last_error && <div className="text-xs text-red-400 break-words">{job.last_error}</div>}
    </div>
  );
}
