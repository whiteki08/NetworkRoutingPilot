package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/yifans/NetworkPilot/backend/internal/model"
)

var ErrNotFound = errors.New("not found")

type Store interface {
	CreateJob(ctx context.Context, domains []string) (model.ProbeJob, error)
	GetJob(ctx context.Context, id string) (model.ProbeJob, error)
	UpdateJob(ctx context.Context, job model.ProbeJob) error
	SaveTrace(ctx context.Context, jobID string, result model.TraceResult) error
	GetJobTraces(ctx context.Context, jobID string) ([]model.TraceResult, error)
	ListOptimizedRules(ctx context.Context) ([]model.DomainRule, error)
}

type MemoryStore struct {
	mu        sync.Mutex
	jobs      map[string]model.ProbeJob
	jobTraces map[string][]model.TraceResult
	latest    map[string]model.TraceResult
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		jobs:      make(map[string]model.ProbeJob),
		jobTraces: make(map[string][]model.TraceResult),
		latest:    make(map[string]model.TraceResult),
	}
}

func (s *MemoryStore) CreateJob(ctx context.Context, domains []string) (model.ProbeJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	job := model.ProbeJob{
		ID:        newID(),
		Status:    model.JobQueued,
		Total:     len(domains),
		CreatedAt: now,
		UpdatedAt: now,
		Counts:    emptyCounts(),
	}
	s.jobs[job.ID] = job
	return job, nil
}

func (s *MemoryStore) GetJob(ctx context.Context, id string) (model.ProbeJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	job, ok := s.jobs[id]
	if !ok {
		return model.ProbeJob{}, ErrNotFound
	}
	return job, nil
}

func (s *MemoryStore) UpdateJob(ctx context.Context, job model.ProbeJob) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.jobs[job.ID]; !ok {
		return ErrNotFound
	}
	job.UpdatedAt = time.Now().UTC()
	s.jobs[job.ID] = job
	return nil
}

func (s *MemoryStore) SaveTrace(ctx context.Context, jobID string, result model.TraceResult) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.jobs[jobID]; !ok {
		return ErrNotFound
	}
	s.jobTraces[jobID] = append(s.jobTraces[jobID], result)
	s.latest[result.TargetDomain] = result
	return nil
}

func (s *MemoryStore) GetJobTraces(ctx context.Context, jobID string) ([]model.TraceResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.jobs[jobID]; !ok {
		return nil, ErrNotFound
	}
	traces := make([]model.TraceResult, len(s.jobTraces[jobID]))
	copy(traces, s.jobTraces[jobID])
	sort.Slice(traces, func(i, j int) bool {
		return traces[i].TargetDomain < traces[j].TargetDomain
	})
	return traces, nil
}

func (s *MemoryStore) ListOptimizedRules(ctx context.Context) ([]model.DomainRule, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rules := make([]model.DomainRule, 0, len(s.latest))
	for _, result := range s.latest {
		rules = append(rules, model.DomainRule{
			Domain: result.TargetDomain,
			Status: result.ClassificationStatus,
			IPv4:   result.ResolvedIPv4,
		})
	}
	sort.Slice(rules, func(i, j int) bool { return rules[i].Domain < rules[j].Domain })
	return rules, nil
}

func emptyCounts() map[model.ClassificationStatus]int {
	return map[model.ClassificationStatus]int{
		model.StatusIEPLDirect:   0,
		model.StatusCN2Premium:   0,
		model.StatusCernetDetour: 0,
		model.StatusBlocked:      0,
		model.StatusStandard163:  0,
	}
}

func newID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return hex.EncodeToString([]byte(time.Now().UTC().Format("20060102150405.000000000")))
	}
	return hex.EncodeToString(b[:])
}

func EncodeTrace(result model.TraceResult) ([]byte, error) {
	return json.Marshal(result)
}

func DecodeTrace(payload []byte) (model.TraceResult, error) {
	var result model.TraceResult
	err := json.Unmarshal(payload, &result)
	return result, err
}
