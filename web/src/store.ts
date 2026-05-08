import { create } from 'zustand';
import type { ClassificationStatus, DomainRule, ProbeJob, TraceResult } from './types';

interface AppState {
  job: ProbeJob | null;
  results: TraceResult[];
  rules: DomainRule[];
  selectedDomain: string | null;
  setJob: (job: ProbeJob | null) => void;
  setResults: (results: TraceResult[]) => void;
  setRules: (rules: DomainRule[]) => void;
  selectDomain: (domain: string | null) => void;
}

export const useAppStore = create<AppState>((set) => ({
  job: null,
  results: [],
  rules: [],
  selectedDomain: null,
  setJob: (job) => set({ job }),
  setResults: (results) => set({ results }),
  setRules: (rules) => set({ rules }),
  selectDomain: (selectedDomain) => set({ selectedDomain }),
}));

export const STATUS_COLORS: Record<ClassificationStatus, string> = {
  IEPL_Direct: '#22c55e',
  CN2_Premium: '#38bdf8',
  CERNET_Detour: '#a78bfa',
  Blocked: '#ef4444',
  Standard_163: '#facc15',
};

export const STATUS_LABELS: Record<ClassificationStatus, string> = {
  IEPL_Direct: 'IEPL Direct',
  CN2_Premium: 'CN2 Premium',
  CERNET_Detour: 'CERNET Detour',
  Blocked: 'Blocked',
  Standard_163: 'Standard 163',
};
