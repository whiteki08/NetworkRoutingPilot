package probe

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/yifans/NetworkPilot/backend/internal/model"
)

// NexttraceProber shells out to the `nexttrace` binary and parses its JSON
// output. nexttrace already solves the hard parts (raw-socket/pcap mechanics,
// ICMP/TCP/UDP modes, ICMP correlation, MPLS, IPv6), so we just consume its
// output.
//
// Requires `nexttrace` (https://github.com/nxtrace/NTrace-core) to be on PATH
// inside the backend container.
type NexttraceProber struct {
	Binary    string // defaults to "nexttrace"
	Mode      string // "icmp" (default), "tcp", "udp"
	Port      int    // tcp/udp port; ignored for icmp
	Queries   int    // probes per hop, default 1
	Parallel  int    // concurrent ttl groups, default nexttrace default
	Interface string // optional -source override via --dev (not wired by default)
}

type ntHop struct {
	Success  bool    `json:"Success"`
	Address  *ntAddr `json:"Address"`
	Hostname string  `json:"Hostname"`
	TTL      int     `json:"TTL"`
	RTT      int64   `json:"RTT"` // nanoseconds
	Geo      *ntGeo  `json:"Geo"`
}

type ntAddr struct {
	IP string `json:"IP"`
}

type ntGeo struct {
	ASNumber string `json:"asnumber"`
	Country  string `json:"country_en"`
	City     string `json:"city_en"`
	ISP      string `json:"isp"`
	Owner    string `json:"owner"`
	Lat      float64 `json:"lat"`
	Lng      float64 `json:"lng"`
}

type ntOutput struct {
	Hops [][]ntHop `json:"Hops"`
}

func NewNexttraceProber(mode string, iface string) *NexttraceProber {
	if mode == "" {
		mode = "icmp"
	}
	return &NexttraceProber{
		Binary:    "nexttrace",
		Mode:      strings.ToLower(mode),
		Queries:   1,
		Interface: iface,
	}
}

func (p *NexttraceProber) Probe(ctx context.Context, target Target) ([]model.Hop, error) {
	if target.IPv4 == nil {
		return nil, errors.New("target IPv4 required")
	}
	bin := p.Binary
	if bin == "" {
		bin = "nexttrace"
	}
	maxHops := target.MaxTTL
	if maxHops <= 0 {
		maxHops = 30
	}
	args := []string{"-4", "-j", "-n", "-M", "--table=false"}
	switch p.Mode {
	case "tcp":
		args = append(args, "-T")
		if p.Port > 0 {
			args = append(args, "-p", strconv.Itoa(p.Port))
		}
	case "udp":
		args = append(args, "-U")
		if p.Port > 0 {
			args = append(args, "-p", strconv.Itoa(p.Port))
		}
	default: // icmp
		// icmp is nexttrace's default when neither -T nor -U is passed
	}
	if p.Queries > 0 {
		args = append(args, "-q", strconv.Itoa(p.Queries))
	}
	args = append(args, "-m", strconv.Itoa(maxHops))
	args = append(args, target.IPv4.String())

	cmd := exec.CommandContext(ctx, bin, args...)
	out, err := cmd.Output()
	if err != nil {
		var ex *exec.ExitError
		if errors.As(err, &ex) {
			return nil, fmt.Errorf("nexttrace exit %d: %s", ex.ExitCode(), strings.TrimSpace(string(ex.Stderr)))
		}
		return nil, fmt.Errorf("run nexttrace: %w", err)
	}
	// nexttrace emits exactly one JSON object on stdout. Some builds prepend
	// log lines; find the last '{' that parses.
	payload := extractJSON(out)
	if payload == nil {
		return nil, fmt.Errorf("nexttrace produced no JSON (output: %q)", string(out))
	}
	var decoded ntOutput
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return nil, fmt.Errorf("parse nexttrace json: %w", err)
	}
	hops := make([]model.Hop, 0, len(decoded.Hops))
	for ttl, samples := range decoded.Hops {
		h := model.Hop{TTL: ttl + 1, Responded: false}
		// Pick the first successful sample for this TTL; fall back to unresponded.
		for _, s := range samples {
			if s.Success && s.Address != nil && s.Address.IP != "" {
				h.IP = s.Address.IP
				h.RTTMS = float64(s.RTT) / 1e6
				h.Responded = true
				if s.Geo != nil {
					if asn, err := strconv.Atoi(s.Geo.ASNumber); err == nil {
						h.ASN = uint(asn)
					}
					h.City = s.Geo.City
					h.Latitude = s.Geo.Lat
					h.Longitude = s.Geo.Lng
					// Geo.Country is the long country name; CountryCode is set
					// from MMDB enrichment later, so we leave it empty here.
				}
				break
			}
		}
		hops = append(hops, h)
	}
	return hops, nil
}

func extractJSON(out []byte) []byte {
	// Fast path: output is pure JSON.
	trimmed := []byte(strings.TrimSpace(string(out)))
	if len(trimmed) > 0 && trimmed[0] == '{' {
		return trimmed
	}
	// Fallback: find last '{' that parses as JSON.
	s := string(out)
	for i := strings.LastIndex(s, "{"); i >= 0; i = strings.LastIndex(s[:i], "{") {
		candidate := []byte(s[i:])
		var tmp any
		if json.Unmarshal(candidate, &tmp) == nil {
			return candidate
		}
		if i == 0 {
			break
		}
	}
	return nil
}
