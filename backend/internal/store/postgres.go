package store

import (
	"context"
	"encoding/json"
	"sort"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yifans/NetworkPilot/backend/internal/model"
)

type PostgresStore struct {
	pool *pgxpool.Pool
}

func NewPostgresStore(ctx context.Context, databaseURL string) (*PostgresStore, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, err
	}
	store := &PostgresStore{pool: pool}
	if err := store.init(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return store, nil
}

func (s *PostgresStore) Close() {
	s.pool.Close()
}

func (s *PostgresStore) init(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `
CREATE TABLE IF NOT EXISTS probe_jobs (
	id TEXT PRIMARY KEY,
	status TEXT NOT NULL,
	total INTEGER NOT NULL,
	processed INTEGER NOT NULL,
	counts JSONB NOT NULL DEFAULT '{}'::jsonb,
	last_error TEXT NOT NULL DEFAULT '',
	created_at TIMESTAMPTZ NOT NULL,
	updated_at TIMESTAMPTZ NOT NULL
);
CREATE TABLE IF NOT EXISTS trace_results (
	job_id TEXT NOT NULL REFERENCES probe_jobs(id) ON DELETE CASCADE,
	target_domain TEXT NOT NULL,
	resolved_ip_v4 TEXT NOT NULL,
	classification_status TEXT NOT NULL,
	result JSONB NOT NULL,
	trace_timestamp TIMESTAMPTZ NOT NULL,
	PRIMARY KEY (job_id, target_domain)
);
CREATE INDEX IF NOT EXISTS idx_trace_latest_domain ON trace_results(target_domain, trace_timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_trace_status ON trace_results(classification_status);
`)
	return err
}

func (s *PostgresStore) CreateJob(ctx context.Context, domains []string) (model.ProbeJob, error) {
	now := time.Now().UTC()
	job := model.ProbeJob{
		ID:        newID(),
		Status:    model.JobQueued,
		Total:     len(domains),
		CreatedAt: now,
		UpdatedAt: now,
		Counts:    emptyCounts(),
	}
	counts, _ := json.Marshal(job.Counts)
	_, err := s.pool.Exec(ctx, `
INSERT INTO probe_jobs (id, status, total, processed, counts, last_error, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
`, job.ID, job.Status, job.Total, job.Processed, counts, job.LastError, job.CreatedAt, job.UpdatedAt)
	return job, err
}

func (s *PostgresStore) GetJob(ctx context.Context, id string) (model.ProbeJob, error) {
	var job model.ProbeJob
	var status string
	var counts []byte
	err := s.pool.QueryRow(ctx, `
SELECT id, status, total, processed, counts, last_error, created_at, updated_at
FROM probe_jobs WHERE id = $1
`, id).Scan(&job.ID, &status, &job.Total, &job.Processed, &counts, &job.LastError, &job.CreatedAt, &job.UpdatedAt)
	if err != nil {
		return model.ProbeJob{}, err
	}
	job.Status = model.ProbeJobStatus(status)
	job.Counts = emptyCounts()
	_ = json.Unmarshal(counts, &job.Counts)
	return job, nil
}

func (s *PostgresStore) UpdateJob(ctx context.Context, job model.ProbeJob) error {
	job.UpdatedAt = time.Now().UTC()
	counts, _ := json.Marshal(job.Counts)
	_, err := s.pool.Exec(ctx, `
UPDATE probe_jobs SET status = $2, processed = $3, counts = $4, last_error = $5, updated_at = $6
WHERE id = $1
`, job.ID, job.Status, job.Processed, counts, job.LastError, job.UpdatedAt)
	return err
}

func (s *PostgresStore) SaveTrace(ctx context.Context, jobID string, result model.TraceResult) error {
	payload, err := json.Marshal(result)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `
INSERT INTO trace_results (job_id, target_domain, resolved_ip_v4, classification_status, result, trace_timestamp)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (job_id, target_domain) DO UPDATE
SET resolved_ip_v4 = EXCLUDED.resolved_ip_v4,
	classification_status = EXCLUDED.classification_status,
	result = EXCLUDED.result,
	trace_timestamp = EXCLUDED.trace_timestamp
`, jobID, result.TargetDomain, result.ResolvedIPv4, result.ClassificationStatus, payload, result.TraceTimestamp)
	return err
}

func (s *PostgresStore) GetJobTraces(ctx context.Context, jobID string) ([]model.TraceResult, error) {
	rows, err := s.pool.Query(ctx, `SELECT result FROM trace_results WHERE job_id = $1 ORDER BY target_domain`, jobID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var results []model.TraceResult
	for rows.Next() {
		var payload []byte
		if err := rows.Scan(&payload); err != nil {
			return nil, err
		}
		result, err := DecodeTrace(payload)
		if err != nil {
			return nil, err
		}
		results = append(results, result)
	}
	return results, rows.Err()
}

func (s *PostgresStore) ListOptimizedRules(ctx context.Context) ([]model.DomainRule, error) {
	rows, err := s.pool.Query(ctx, `
WITH latest AS (
	SELECT DISTINCT ON (target_domain)
		target_domain, resolved_ip_v4, classification_status, trace_timestamp
	FROM trace_results
	ORDER BY target_domain, trace_timestamp DESC
)
SELECT target_domain, resolved_ip_v4, classification_status
FROM latest
WHERE classification_status IN ($1, $2)
ORDER BY target_domain
`, model.StatusIEPLDirect, model.StatusCN2Premium)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var rules []model.DomainRule
	for rows.Next() {
		var rule model.DomainRule
		var status string
		if err := rows.Scan(&rule.Domain, &rule.IPv4, &status); err != nil {
			return nil, err
		}
		rule.Status = model.ClassificationStatus(status)
		rules = append(rules, rule)
	}
	sort.Slice(rules, func(i, j int) bool { return rules[i].Domain < rules[j].Domain })
	return rules, rows.Err()
}
