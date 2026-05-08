package model

import "time"

type ClassificationStatus string

const (
	StatusIEPLDirect  ClassificationStatus = "IEPL_Direct"
	StatusCN2Premium ClassificationStatus = "CN2_Premium"
	StatusCernetDetour ClassificationStatus = "CERNET_Detour"
	StatusBlocked     ClassificationStatus = "Blocked"
	StatusStandard163 ClassificationStatus = "Standard_163"
)

type Hop struct {
	TTL         int     `json:"ttl"`
	IP          string  `json:"ip,omitempty"`
	CountryCode string  `json:"country_code,omitempty"`
	City        string  `json:"city,omitempty"`
	Latitude    float64 `json:"latitude,omitempty"`
	Longitude   float64 `json:"longitude,omitempty"`
	ASN         uint    `json:"asn,omitempty"`
	RTTMS       float64 `json:"rtt_ms,omitempty"`
	Responded   bool    `json:"responded"`
}

type PathMetrics struct {
	TotalHops        int     `json:"total_hops"`
	DestinationRTTMS float64 `json:"destination_rtt_ms"`
	BorderDeltaMS    float64 `json:"border_delta_ms,omitempty"`
	ASNSequence      []uint  `json:"asn_sequence"`
}

type TraceResult struct {
	TargetDomain         string               `json:"target_domain"`
	ResolvedIPv4         string               `json:"resolved_ip_v4"`
	ClassificationStatus ClassificationStatus `json:"classification_status"`
	PathMetrics          PathMetrics          `json:"path_metrics"`
	Hops                 []Hop                `json:"hops"`
	TraceTimestamp       time.Time            `json:"trace_timestamp"`
	Error                string               `json:"error,omitempty"`
}

type ProbeJobStatus string

const (
	JobQueued    ProbeJobStatus = "queued"
	JobRunning   ProbeJobStatus = "running"
	JobCompleted ProbeJobStatus = "completed"
	JobFailed    ProbeJobStatus = "failed"
)

type ProbeJob struct {
	ID          string                       `json:"id"`
	Status      ProbeJobStatus              `json:"status"`
	Total       int                          `json:"total"`
	Processed   int                          `json:"processed"`
	CreatedAt   time.Time                    `json:"created_at"`
	UpdatedAt   time.Time                    `json:"updated_at"`
	Counts      map[ClassificationStatus]int `json:"counts"`
	LastError   string                       `json:"last_error,omitempty"`
}

type DomainRule struct {
	Domain string               `json:"domain"`
	Status ClassificationStatus `json:"status"`
	IPv4   string               `json:"ipv4,omitempty"`
}
