package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	"github.com/yifans/NetworkPilot/backend/internal/classifier"
	"github.com/yifans/NetworkPilot/backend/internal/geoip"
	"github.com/yifans/NetworkPilot/backend/internal/model"
	"github.com/yifans/NetworkPilot/backend/internal/probe"
	"github.com/yifans/NetworkPilot/backend/internal/resolver"
	"github.com/yifans/NetworkPilot/backend/internal/store"
)

type Orchestrator struct {
	Store       store.Store
	Resolver    resolver.IPv4Resolver
	Prober      probe.Prober
	Enricher    geoip.Enricher
	Concurrency int
	MaxTTL      int
	ProbeTimeout time.Duration
	Logger      *log.Logger
}

type domainTask struct {
	domain string
}

type traceOutcome struct {
	result model.TraceResult
	err    error
}

func (o *Orchestrator) EnqueueJob(ctx context.Context, domains []string) (model.ProbeJob, error) {
	if len(domains) == 0 {
		return model.ProbeJob{}, errors.New("no domains to probe")
	}
	unique := dedupeDomains(domains)
	job, err := o.Store.CreateJob(ctx, unique)
	if err != nil {
		return model.ProbeJob{}, err
	}
	go o.runJob(context.Background(), job.ID, unique)
	return job, nil
}

func (o *Orchestrator) runJob(ctx context.Context, jobID string, domains []string) {
	job, err := o.Store.GetJob(ctx, jobID)
	if err != nil {
		o.logf("orchestrator: load job %s: %v", jobID, err)
		return
	}
	job.Status = model.JobRunning
	if err := o.Store.UpdateJob(ctx, job); err != nil {
		o.logf("orchestrator: mark running %s: %v", jobID, err)
	}

	concurrency := o.Concurrency
	if concurrency <= 0 {
		concurrency = 32
	}
	tasks := make(chan domainTask)
	results := make(chan traceOutcome)

	var wg sync.WaitGroup
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for task := range tasks {
				result, err := o.traceDomain(ctx, task.domain)
				results <- traceOutcome{result: result, err: err}
			}
		}()
	}

	go func() {
		defer close(tasks)
		for _, domain := range domains {
			select {
			case <-ctx.Done():
				return
			case tasks <- domainTask{domain: domain}:
			}
		}
	}()

	go func() {
		wg.Wait()
		close(results)
	}()

	counts := emptyCounts()
	processed := 0
	var lastErr string
	for outcome := range results {
		processed++
		if outcome.err != nil {
			lastErr = outcome.err.Error()
			outcome.result.Error = lastErr
			if outcome.result.TraceTimestamp.IsZero() {
				outcome.result.TraceTimestamp = time.Now().UTC()
			}
		}
		if outcome.result.ClassificationStatus != "" {
			counts[outcome.result.ClassificationStatus]++
		}
		if outcome.result.TargetDomain != "" {
			if err := o.Store.SaveTrace(ctx, jobID, outcome.result); err != nil {
				o.logf("orchestrator: save trace %s: %v", outcome.result.TargetDomain, err)
			}
		}
		job.Processed = processed
		job.Counts = counts
		job.LastError = lastErr
		if err := o.Store.UpdateJob(ctx, job); err != nil {
			o.logf("orchestrator: update progress %s: %v", jobID, err)
		}
	}

	job.Status = model.JobCompleted
	job.Counts = counts
	if err := o.Store.UpdateJob(ctx, job); err != nil {
		o.logf("orchestrator: finalize %s: %v", jobID, err)
	}
}

func (o *Orchestrator) traceDomain(ctx context.Context, domain string) (model.TraceResult, error) {
	resolveCtx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	ip, err := o.Resolver.LookupIPv4(resolveCtx, domain)
	if err != nil {
		return model.TraceResult{
			TargetDomain:         domain,
			ClassificationStatus: model.StatusBlocked,
			PathMetrics:          model.PathMetrics{ASNSequence: []uint{}},
			Hops:                 []model.Hop{},
			TraceTimestamp:       time.Now().UTC(),
		}, fmt.Errorf("resolve %s: %w", domain, err)
	}

	target := probe.Target{Domain: domain, IPv4: ip, MaxTTL: o.MaxTTL}
	probeCtx, probeCancel := context.WithTimeout(ctx, o.probeTimeout())
	defer probeCancel()

	hops, probeErr := o.Prober.Probe(probeCtx, target)
	enriched := o.enrichHops(hops)
	result := classifier.Classify(domain, ip.String(), enriched)
	if probeErr != nil {
		return result, fmt.Errorf("probe %s: %w", domain, probeErr)
	}
	return result, nil
}

func (o *Orchestrator) enrichHops(hops []model.Hop) []model.Hop {
	if o.Enricher == nil {
		return hops
	}
	out := make([]model.Hop, len(hops))
	for i, hop := range hops {
		if hop.IP == "" || net.ParseIP(hop.IP) == nil {
			out[i] = hop
			continue
		}
		out[i] = o.Enricher.EnrichHop(hop)
	}
	return out
}

func (o *Orchestrator) probeTimeout() time.Duration {
	if o.ProbeTimeout <= 0 {
		return 45 * time.Second
	}
	return o.ProbeTimeout
}

func (o *Orchestrator) logf(format string, args ...any) {
	if o.Logger != nil {
		o.Logger.Printf(format, args...)
	}
}

func dedupeDomains(domains []string) []string {
	seen := make(map[string]bool)
	out := make([]string, 0, len(domains))
	for _, domain := range domains {
		if domain == "" || seen[domain] {
			continue
		}
		seen[domain] = true
		out = append(out, domain)
	}
	return out
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
