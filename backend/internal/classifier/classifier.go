package classifier

import (
	"strings"
	"time"

	"github.com/yifans/NetworkPilot/backend/internal/model"
)

// ieplBorderDeltaUpperBoundMS is the max acceptable RTT jump across the
// CN-border when the path stays on a Chinese carrier's AS. A true IEPL/IPLC
// circuit adds almost nothing to RTT because it's a dedicated L2 link; public
// internet egress normally adds tens of ms just from the first transit hop.
const ieplBorderDeltaUpperBoundMS = 5.0

// chinaCarrierOverseasAS: Chinese carriers' ASes that serve as the overseas
// end of a dedicated circuit (IEPL / IPLC / CN2 / CMI). When the hop right
// after a CN hop is still in one of these ASes, the crossing is by dedicated
// circuit rather than handed off to a foreign transit network.
var chinaCarrierOverseasAS = map[uint]bool{
	4134:  true, // ChinaNet (CT 163 + overseas PoPs)
	4809:  true, // China Telecom Next Generation (CN2 / CN2 GIA)
	9929:  true, // China Unicom Premium (CUG)
	4837:  true, // China Unicom 169 backbone
	58453: true, // China Mobile International
	9808:  true, // China Mobile CMNet / CMI legacy
	58807: true, // China Telecom Global
	10103: true, // China Telecom Global (HK)
}

// cn2PremiumAS: specifically CN2 GIA / GT (a subset of the China-carrier
// group worth calling out as "CN2 Premium").
var cn2PremiumAS = map[uint]bool{
	4809: true,
}

// cernetAS: China Education & Research Network. Traffic that leaves mainland
// China through CERNET is the "CERNET Detour" case, distinct from the three
// commercial operators.
var cernetAS = map[uint]bool{
	4538:  true, // CERNET backbone
	23910: true, // CERNET2 / NGI
}

// foreignTransitAS: global tier-1/2 transit carriers. If the first non-CN
// hop belongs to one of these, the path is *not* on a dedicated Chinese
// carrier circuit — it's been handed off to the public internet, i.e.
// Standard (163) rather than IEPL.
var foreignTransitAS = map[uint]bool{
	2914: true, // NTT Communications
	1299: true, // Arelion (ex-Telia)
	3356: true, // Lumen / Level 3
	174:  true, // Cogent
	3257: true, // GTT
	6453: true, // Tata Communications
	6939: true, // Hurricane Electric
	701:  true, // Verizon UUNET
	1273: true, // Vodafone
	5511: true, // Orange
	6762: true, // Telecom Italia Sparkle
}

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

	// Order matters: check for dedicated-circuit signatures (IEPL, CN2)
	// before falling back to generic signals. A path through both CN2 (4809)
	// and CERNET would be misclassified as CERNET if we checked that first.
	if cn2 := firstASInSet(metrics.ASNSequence, cn2PremiumAS); cn2 {
		// CN2 Premium only if the border handoff stays within a Chinese
		// carrier's AS; if the trace hits NTT/Telia right after leaving
		// 4809, it's Standard.
		if isChineseCarrierBorder(hops, &metrics) {
			return model.StatusCN2Premium, metrics
		}
	}
	if isIEPLDirect(hops, &metrics) {
		return model.StatusIEPLDirect, metrics
	}
	if containsAnyAS(metrics.ASNSequence, cernetAS) {
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

// isIEPLDirect returns true when the path crosses the CN border while
// staying entirely on a Chinese carrier's AS. Checks:
//  1. A CN → non-CN country transition exists.
//  2. The first overseas hop's ASN is in chinaCarrierOverseasAS (not
//     handed off to a foreign transit like NTT/Telia).
//  3. No foreign-transit AS appears anywhere in the full AS sequence
//     (a true IEPL circuit terminates at the carrier's overseas PoP and
//     hands off directly to the destination AS).
//  4. Border RTT delta is small (dedicated circuit, no re-transit).
func isIEPLDirect(hops []model.Hop, metrics *model.PathMetrics) bool {
	if containsAnyAS(metrics.ASNSequence, foreignTransitAS) {
		return false
	}
	for i := 0; i < len(hops)-1; i++ {
		cur, next := hops[i], hops[i+1]
		if !cur.Responded || !next.Responded {
			continue
		}
		if !strings.EqualFold(cur.CountryCode, "CN") {
			continue
		}
		if next.CountryCode == "" || strings.EqualFold(next.CountryCode, "CN") {
			continue
		}
		// The overseas-side AS must be a Chinese carrier's international AS.
		if !chinaCarrierOverseasAS[next.ASN] {
			return false
		}
		delta := next.RTTMS - cur.RTTMS
		if delta >= 0 && delta <= ieplBorderDeltaUpperBoundMS {
			metrics.BorderDeltaMS = delta
			return true
		}
		return false
	}
	return false
}

// isChineseCarrierBorder is a laxer version of isIEPLDirect used to
// validate a CN2 classification: same structure but doesn't require the
// absence of foreign transit further down the path (CN2 GIA hands off to
// Tier-1 transit after landing overseas, that's normal).
func isChineseCarrierBorder(hops []model.Hop, metrics *model.PathMetrics) bool {
	for i := 0; i < len(hops)-1; i++ {
		cur, next := hops[i], hops[i+1]
		if !cur.Responded || !next.Responded {
			continue
		}
		if !strings.EqualFold(cur.CountryCode, "CN") {
			continue
		}
		if next.CountryCode == "" || strings.EqualFold(next.CountryCode, "CN") {
			continue
		}
		if chinaCarrierOverseasAS[next.ASN] {
			delta := next.RTTMS - cur.RTTMS
			if delta >= 0 {
				metrics.BorderDeltaMS = delta
			}
			return true
		}
		return false
	}
	return false
}

func isBlocked(hops []model.Hop) bool {
	if len(hops) == 0 {
		return true
	}
	consecutiveLoss := 0
	for _, hop := range hops {
		if hop.Responded {
			consecutiveLoss = 0
			continue
		}
		consecutiveLoss++
		if consecutiveLoss >= 3 {
			return true
		}
	}
	return false
}

func containsAnyAS(asns []uint, set map[uint]bool) bool {
	for _, asn := range asns {
		if set[asn] {
			return true
		}
	}
	return false
}

func firstASInSet(asns []uint, set map[uint]bool) bool {
	for _, asn := range asns {
		if set[asn] {
			return true
		}
	}
	return false
}
