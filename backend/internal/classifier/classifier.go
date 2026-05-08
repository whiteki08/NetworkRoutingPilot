package classifier

import (
	"strings"
	"time"

	"github.com/yifans/NetworkPilot/backend/internal/model"
)

const ieplDeltaUpperBoundMS = 3.0

func Classify(domain string, resolvedIPv4 string, hops []model.Hop) model.TraceResult {
	status, metrics := classifyPath(hops)
	return model.TraceResult{
		TargetDomain:         domain,
		ResolvedIPv4:         resolvedIPv4,
		ClassificationStatus: status,
		PathMetrics:          metrics,
		Hops:                 hops,
		TraceTimestamp:       time.Now().UTC(),
	}
}

func classifyPath(hops []model.Hop) (model.ClassificationStatus, model.PathMetrics) {
	metrics := buildMetrics(hops)
	if isIEPLDirect(hops, &metrics) {
		return model.StatusIEPLDirect, metrics
	}
	if containsASN(metrics.ASNSequence, 4809) {
		return model.StatusCN2Premium, metrics
	}
	if containsASN(metrics.ASNSequence, 4538) || containsASN(metrics.ASNSequence, 23910) {
		return model.StatusCernetDetour, metrics
	}
	if isBlocked(hops) {
		return model.StatusBlocked, metrics
	}
	return model.StatusStandard163, metrics
}

func buildMetrics(hops []model.Hop) model.PathMetrics {
	asnSeen := make(map[uint]bool)
	asnSequence := make([]uint, 0, len(hops))
	metrics := model.PathMetrics{TotalHops: len(hops)}
	for _, hop := range hops {
		if hop.Responded && hop.RTTMS > 0 {
			metrics.DestinationRTTMS = hop.RTTMS
		}
		if hop.ASN != 0 && !asnSeen[hop.ASN] {
			asnSeen[hop.ASN] = true
			asnSequence = append(asnSequence, hop.ASN)
		}
	}
	metrics.ASNSequence = asnSequence
	return metrics
}

func isIEPLDirect(hops []model.Hop, metrics *model.PathMetrics) bool {
	for i := 0; i < len(hops)-1; i++ {
		current := hops[i]
		next := hops[i+1]
		if !current.Responded || !next.Responded {
			continue
		}
		if strings.EqualFold(current.CountryCode, "CN") && !strings.EqualFold(next.CountryCode, "CN") && next.CountryCode != "" {
			delta := next.RTTMS - current.RTTMS
			if delta > 0 && delta <= ieplDeltaUpperBoundMS {
				metrics.BorderDeltaMS = delta
				return true
			}
		}
	}
	return false
}

func isBlocked(hops []model.Hop) bool {
	if len(hops) == 0 {
		return true
	}
	responded := 0
	consecutiveLoss := 0
	for _, hop := range hops {
		if hop.Responded {
			responded++
			consecutiveLoss = 0
			continue
		}
		consecutiveLoss++
		if consecutiveLoss >= 3 {
			return true
		}
	}
	return responded == 0
}

func containsASN(asns []uint, target uint) bool {
	for _, asn := range asns {
		if asn == target {
			return true
		}
	}
	return false
}
