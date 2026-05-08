import { useEffect, useMemo, useRef, useState } from 'react';
import { createProbe, getLatestRules, getProbe, getProbeResults } from './api';
import { useAppStore } from './store';
import ProbeForm from './components/ProbeForm';
import JobProgress from './components/JobProgress';
import ClassificationChart from './components/ClassificationChart';
import ResultsTable from './components/ResultsTable';
import HopMap from './components/HopMap';
import HopDetails from './components/HopDetails';
import DeliveryPanel from './components/DeliveryPanel';
import RipeLookup from './components/RipeLookup';

export default function App() {
  const { job, results, rules, selectedDomain, setJob, setResults, setRules } = useAppStore();
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const pollRef = useRef<number | null>(null);

  useEffect(() => {
    getLatestRules().then(setRules).catch(() => void 0);
  }, [setRules]);

  useEffect(() => {
    if (!job || job.status === 'completed' || job.status === 'failed') {
      if (pollRef.current) {
        window.clearInterval(pollRef.current);
        pollRef.current = null;
      }
      return;
    }
    pollRef.current = window.setInterval(async () => {
      try {
        const updated = await getProbe(job.id);
        setJob(updated);
        const newResults = await getProbeResults(job.id);
        setResults(newResults);
        if (updated.status === 'completed') {
          const latest = await getLatestRules();
          setRules(latest);
        }
      } catch {
        // swallow transient errors
      }
    }, 2000);
    return () => {
      if (pollRef.current) window.clearInterval(pollRef.current);
    };
  }, [job, setJob, setResults, setRules]);

  const onSubmit = async (payload: { domains?: string[]; surge_url?: string; surge_inline?: string }) => {
    setSubmitting(true);
    setError(null);
    try {
      const created = await createProbe(payload);
      setJob(created);
      setResults([]);
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setSubmitting(false);
    }
  };

  const selectedResult = useMemo(
    () => results.find((r) => r.target_domain === selectedDomain) ?? null,
    [results, selectedDomain],
  );

  const statusCounts = useMemo(() => {
    const counts = { IEPL_Direct: 0, CN2_Premium: 0, CERNET_Detour: 0, Blocked: 0, Standard_163: 0 } as const;
    const mutable: Record<string, number> = { ...counts };
    for (const r of results) {
      mutable[r.classification_status] = (mutable[r.classification_status] ?? 0) + 1;
    }
    return mutable;
  }, [results]);

  return (
    <div className="min-h-screen p-6 grid grid-cols-12 gap-4">
      <header className="col-span-12 flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold tracking-tight">NetworkPilot</h1>
          <p className="text-slate-400 text-sm">Adaptive PBR Probe System — classify, visualize, deliver routing.</p>
        </div>
        <div className="flex items-center gap-2 text-sm text-slate-400">
          <span className="pill bg-ink-700 text-slate-300">Build · Dev</span>
          <span className="pill bg-ink-700 text-slate-300">{rules.length} optimized rules</span>
        </div>
      </header>

      <section className="col-span-12 lg:col-span-4 space-y-4">
        <ProbeForm onSubmit={onSubmit} submitting={submitting} error={error} />
        <JobProgress job={job} />
        <ClassificationChart counts={statusCounts} />
        <DeliveryPanel />
      </section>

      <section className="col-span-12 lg:col-span-8 space-y-4">
        <HopMap result={selectedResult} />
        <div className="grid grid-cols-1 xl:grid-cols-2 gap-4">
          <ResultsTable results={results} />
          <HopDetails result={selectedResult} />
        </div>
        <RipeLookup />
      </section>
    </div>
  );
}
