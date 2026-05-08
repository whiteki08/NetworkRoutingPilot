package delivery

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/yifans/NetworkPilot/backend/internal/model"
)

var domainPattern = regexp.MustCompile(`^[A-Za-z0-9.-]+$`)

type XrayRoutingObject struct {
	DomainStrategy string     `json:"domainStrategy,omitempty"`
	Rules          []XrayRule `json:"rules"`
}

type XrayRule struct {
	Type        string   `json:"type"`
	OutboundTag string   `json:"outboundTag"`
	Domain      []string `json:"domain,omitempty"`
	IP          []string `json:"ip,omitempty"`
}

func OptimizedDomains(rules []model.DomainRule) []string {
	seen := make(map[string]bool)
	domains := make([]string, 0, len(rules))
	for _, rule := range rules {
		if rule.Status != model.StatusIEPLDirect && rule.Status != model.StatusCN2Premium {
			continue
		}
		domain := normalizeDomain(rule.Domain)
		if domain == "" || seen[domain] {
			continue
		}
		seen[domain] = true
		domains = append(domains, domain)
	}
	sort.Strings(domains)
	return domains
}

func RenderSurgeRuleset(rules []model.DomainRule) (string, error) {
	domains := OptimizedDomains(rules)
	lines := make([]string, 0, len(domains))
	for _, domain := range domains {
		if !domainPattern.MatchString(domain) {
			return "", fmt.Errorf("invalid domain for Surge ruleset: %s", domain)
		}
		lines = append(lines, "DOMAIN-SUFFIX,"+domain)
	}
	if len(lines) == 0 {
		return "", nil
	}
	return strings.Join(lines, "\n") + "\n", nil
}

func RenderXrayRouting(rules []model.DomainRule) ([]byte, error) {
	domains := OptimizedDomains(rules)
	xrayDomains := make([]string, 0, len(domains))
	for _, domain := range domains {
		if !domainPattern.MatchString(domain) {
			return nil, fmt.Errorf("invalid domain for Xray routing: %s", domain)
		}
		xrayDomains = append(xrayDomains, "domain:"+domain)
	}
	obj := XrayRoutingObject{
		DomainStrategy: "AsIs",
		Rules: []XrayRule{
			{
				Type:        "field",
				OutboundTag: "direct",
				Domain:      xrayDomains,
			},
		},
	}
	return json.MarshalIndent(obj, "", "  ")
}

func ETag(content []byte) string {
	sum := sha256.Sum256(content)
	return `"` + hex.EncodeToString(sum[:]) + `"`
}

func normalizeDomain(domain string) string {
	domain = strings.TrimSpace(strings.ToLower(domain))
	domain = strings.TrimPrefix(domain, ".")
	return domain
}
