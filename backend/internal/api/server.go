package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/yifans/NetworkPilot/backend/internal/delivery"
	"github.com/yifans/NetworkPilot/backend/internal/orchestrator"
	"github.com/yifans/NetworkPilot/backend/internal/parser"
	"github.com/yifans/NetworkPilot/backend/internal/ripe"
	"github.com/yifans/NetworkPilot/backend/internal/store"
)

type Server struct {
	Store        store.Store
	Orchestrator *orchestrator.Orchestrator
	RIPE         *ripe.Client
	Fetcher      parser.Fetcher
}

func NewRouter(s *Server) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(30 * time.Second))
	r.Use(corsMiddleware)

	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	r.Route("/api/v1", func(r chi.Router) {
		r.Post("/probes", s.createProbe)
		r.Get("/probes/{id}", s.getProbe)
		r.Get("/probes/{id}/results", s.getProbeResults)
		r.Get("/results/latest", s.getLatestResults)
		r.Get("/ripe/{resource}", s.getRIPE)
		r.Get("/delivery/surge/optimized.list", s.getSurgeRuleset)
		r.Get("/delivery/xray/optimized.json", s.getXrayRouting)
	})
	return r
}

type probeRequest struct {
	Domains    []string `json:"domains"`
	SurgeURL   string   `json:"surge_url,omitempty"`
	SurgeInline string  `json:"surge_inline,omitempty"`
}

func (s *Server) createProbe(w http.ResponseWriter, r *http.Request) {
	var req probeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	domains := normalizeDomains(req.Domains)

	if req.SurgeURL != "" {
		fetched, err := s.fetchSurgeDomains(r.Context(), req.SurgeURL)
		if err != nil {
			writeError(w, http.StatusBadGateway, "fetch surge ruleset: "+err.Error())
			return
		}
		domains = append(domains, fetched...)
	}
	if req.SurgeInline != "" {
		inline, err := parser.ParseSurgeDomains(r.Context(), strings.NewReader(req.SurgeInline), s.Fetcher)
		if err != nil {
			writeError(w, http.StatusBadRequest, "parse inline surge: "+err.Error())
			return
		}
		domains = append(domains, inline...)
	}

	domains = dedupe(domains)
	if len(domains) == 0 {
		writeError(w, http.StatusBadRequest, "no domains provided")
		return
	}

	job, err := s.Orchestrator.EnqueueJob(r.Context(), domains)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, job)
}

func (s *Server) fetchSurgeDomains(ctx context.Context, source string) ([]string, error) {
	if s.Fetcher == nil {
		return nil, errors.New("surge fetcher not configured")
	}
	body, err := s.Fetcher.Fetch(ctx, source)
	if err != nil {
		return nil, err
	}
	defer body.Close()
	return parser.ParseSurgeDomains(ctx, body, s.Fetcher)
}

func (s *Server) getProbe(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	job, err := s.Store.GetJob(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "job not found")
		return
	}
	writeJSON(w, http.StatusOK, job)
}

func (s *Server) getProbeResults(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	results, err := s.Store.GetJobTraces(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "job not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": results})
}

func (s *Server) getLatestResults(w http.ResponseWriter, r *http.Request) {
	rules, err := s.Store.ListOptimizedRules(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"rules": rules})
}

func (s *Server) getRIPE(w http.ResponseWriter, r *http.Request) {
	if s.RIPE == nil {
		writeError(w, http.StatusServiceUnavailable, "ripe client not configured")
		return
	}
	resource := chi.URLParam(r, "resource")
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	record, err := s.RIPE.Lookup(ctx, resource)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, record)
}

func (s *Server) getSurgeRuleset(w http.ResponseWriter, r *http.Request) {
	rules, err := s.Store.ListOptimizedRules(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	body, err := delivery.RenderSurgeRuleset(rules)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	header := "# Title: Adaptive PBR Optimized List\n# Generated: " + time.Now().UTC().Format(time.RFC3339) + "\n# Format: Surge RULE-SET\n"
	payload := []byte(header + body)
	writeCacheable(w, r, "text/plain; charset=utf-8", payload)
}

func (s *Server) getXrayRouting(w http.ResponseWriter, r *http.Request) {
	rules, err := s.Store.ListOptimizedRules(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	body, err := delivery.RenderXrayRouting(rules)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeCacheable(w, r, "application/json", body)
}

func writeCacheable(w http.ResponseWriter, r *http.Request, contentType string, payload []byte) {
	etag := delivery.ETag(payload)
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "no-cache")
	if match := r.Header.Get("If-None-Match"); match != "" && match == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Content-Type", contentType)
	_, _ = io.Copy(w, bytes.NewReader(payload))
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func normalizeDomains(input []string) []string {
	out := make([]string, 0, len(input))
	for _, raw := range input {
		domain := strings.TrimSpace(strings.ToLower(raw))
		domain = strings.TrimPrefix(domain, ".")
		if domain != "" {
			out = append(out, domain)
		}
	}
	return out
}

func dedupe(input []string) []string {
	seen := make(map[string]bool)
	out := make([]string, 0, len(input))
	for _, v := range input {
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin == "" {
			origin = "*"
		}
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, If-None-Match")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Expose-Headers", "ETag")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
