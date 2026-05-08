export type ClassificationStatus =
  | 'IEPL_Direct'
  | 'CN2_Premium'
  | 'CERNET_Detour'
  | 'Blocked'
  | 'Standard_163';

export interface Hop {
  ttl: number;
  ip?: string;
  country_code?: string;
  city?: string;
  latitude?: number;
  longitude?: number;
  asn?: number;
  rtt_ms?: number;
  responded: boolean;
}

export interface PathMetrics {
  total_hops: number;
  destination_rtt_ms: number;
  border_delta_ms?: number;
  asn_sequence: number[];
}

export interface TraceResult {
  target_domain: string;
  resolved_ip_v4: string;
  classification_status: ClassificationStatus;
  path_metrics: PathMetrics;
  hops: Hop[];
  trace_timestamp: string;
  error?: string;
}

export interface ProbeJob {
  id: string;
  status: 'queued' | 'running' | 'completed' | 'failed';
  total: number;
  processed: number;
  created_at: string;
  updated_at: string;
  counts: Record<ClassificationStatus, number>;
  last_error?: string;
}

export interface DomainRule {
  domain: string;
  status: ClassificationStatus;
  ipv4?: string;
}

export interface RIPERecord {
  resource: string;
  asn?: number;
  target_prefix?: string;
  communities?: string[];
  source: string;
  cached: boolean;
}
