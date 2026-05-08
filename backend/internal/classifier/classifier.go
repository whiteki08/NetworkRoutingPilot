package classifier

import (
	"net"
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
	status, metrics := classifyPath(resolvedIPv4, hops)
	return model.TraceResult{
		TargetDomain:         domain,
		ResolvedIPv4:         resolvedIPv4,
		ClassificationStatus: status,
		PathMetrics:          metrics,
		Hops:                 hops,
		TraceTimestamp:       time.Now().UTC(),
	}
}

func classifyPath(resolvedIPv4 string, hops []model.Hop) (model.ClassificationStatus, model.PathMetrics) {
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
	if isIEPLDirect(hops, &metrics) || isIEPLPrivateEgress(hops, &metrics) {
		return model.StatusIEPLDirect, metrics
	}
	if containsAnyAS(metrics.ASNSequence, cernetAS) {
		return model.StatusCernetDetour, metrics
	}
	if isBlocked(resolvedIPv4, hops) {
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

// isBlocked returns true only when the trace clearly failed to reach the
// destination. Intermediate silent hops are normal (ICMP rate-limiting on
// many backbone routers), so we don't penalize them — what matters is
// whether the trace eventually landed on the resolved destination IP.
//
// Rules:
//  1. If any responded hop's IP equals the resolved destination, the path
//     reached the target → not blocked.
//  2. Otherwise, if zero hops responded at all → blocked.
//  3. Otherwise, if the last few hops are all silent (no response near the
//     tail of the trace), treat it as blocked — the path trailed off.
// isIEPLPrivateEgress detects the corporate-IEPL pattern where the trace
// leaves a private/RFC1918 LAN inside mainland China and the first public
// hop is already overseas, with no Chinese public-carrier AS in the path.
// That signature — private hops, then a direct jump into a foreign AS —
// is produced by a dedicated L2 circuit (IEPL/IPLC) carrying corp traffic
// straight to an overseas PoP, bypassing the public 163/CN2/CERNET backbones.
//
// Rules:
//  1. At least one early hop is RFC1918/RFC6598 (private) or unresponded.
//  2. The first *public* responded hop has a non-CN country and a non-zero
//     RTT below ieplPrivateEgressMaxRTTMS (pure fiber from mainland to the
//     nearest overseas hub is typically <60ms, certainly <100ms).
//  3. No Chinese public-carrier AS (4134/4809/9929/4837/CERNET/etc.) appears
//     anywhere in the AS sequence — if one did, the path is using a public
//     CN backbone and is not IEPL.
//  4. No foreign tier-1 transit appears before the first overseas hop.
func isIEPLPrivateEgress(hops []model.Hop, metrics *model.PathMetrics) bool {
	// Rule 3: any Chinese public-carrier AS anywhere → not this pattern.
	for _, asn := range metrics.ASNSequence {
		if chinaCarrierOverseasAS[asn] || cernetAS[asn] {
			return false
		}
	}
	sawPrivate := false
	for _, hop := range hops {
		if !hop.Responded {
			continue
		}
		ip := net.ParseIP(hop.IP)
		if ip == nil {
			continue
		}
		if isPrivateOrSharedIP(ip) {
			sawPrivate = true
			continue
		}
		// First public responded hop.
		if strings.EqualFold(hop.CountryCode, "CN") || hop.CountryCode == "" {
			return false
		}
		if hop.RTTMS <= 0 || hop.RTTMS > ieplPrivateEgressMaxRTTMS {
			return false
		}
		if !sawPrivate {
			return false
		}
		metrics.BorderDeltaMS = hop.RTTMS
		return true
	}
	return false
}

// ieplPrivateEgressMaxRTTMS caps the RTT to the first overseas public hop.
// Dedicated fibre from mainland China to HK/SG is typically 30-50ms; public
// transit routinely adds 50ms+ of queueing. 80ms leaves room for Tokyo/US-West
// landing points while still rejecting clearly-detoured paths.
const ieplPrivateEgressMaxRTTMS = 80.0

func isPrivateOrSharedIP(ip net.IP) bool {
	if ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return true
	}
	// RFC6598 shared address space (100.64.0.0/10) used by carrier-grade NAT
	// and on many corp WAN edges; treat as private for this heuristic.
	if ip4 := ip.To4(); ip4 != nil && ip4[0] == 100 && ip4[1] >= 64 && ip4[1] <= 127 {
		return true
	}
	return false
}

func isBlocked(resolvedIPv4 string, hops []model.Hop) bool {
	if len(hops) == 0 {
		return true
	}
	anyResponded := false
	for _, hop := range hops {
		if hop.Responded {
			anyResponded = true
			if resolvedIPv4 != "" && hop.IP == resolvedIPv4 {
				return false
			}
		}
	}
	if !anyResponded {
		return true
	}
	// Tail silence: if the last 5 hops are all unresponded and we never hit
	// the destination IP, the path didn't complete.
	tail := min(5, len(hops))
	for i := len(hops) - tail; i < len(hops); i++ {
		if hops[i].Responded {
			return false
		}
	}
	return true
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
