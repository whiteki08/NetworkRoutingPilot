import axios from 'axios';
import type { DomainRule, ProbeJob, RIPERecord, TraceResult } from './types';

const client = axios.create({
  baseURL: '/api/v1',
  timeout: 30000,
});

export async function createProbe(body: {
  domains?: string[];
  surge_url?: string;
  surge_inline?: string;
}): Promise<ProbeJob> {
  const { data } = await client.post<ProbeJob>('/probes', body);
  return data;
}

export async function getProbe(id: string): Promise<ProbeJob> {
  const { data } = await client.get<ProbeJob>(`/probes/${id}`);
  return data;
}

export async function getProbeResults(id: string): Promise<TraceResult[]> {
  const { data } = await client.get<{ results: TraceResult[] }>(`/probes/${id}/results`);
  return data.results ?? [];
}

export async function getLatestRules(): Promise<DomainRule[]> {
  const { data } = await client.get<{ rules: DomainRule[] }>('/results/latest');
  return data.rules ?? [];
}

export async function lookupRIPE(resource: string): Promise<RIPERecord> {
  const { data } = await client.get<RIPERecord>(`/ripe/${encodeURIComponent(resource)}`);
  return data;
}

export function surgeRulesetURL(): string {
  return '/api/v1/delivery/surge/optimized.list';
}

export function xrayRoutingURL(): string {
  return '/api/v1/delivery/xray/optimized.json';
}
