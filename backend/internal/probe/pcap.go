package probe

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	"github.com/google/gopacket/pcap"
	"github.com/yifans/NetworkPilot/backend/internal/model"
	"golang.org/x/net/ipv4"
)

type Target struct {
	Domain string
	IPv4   net.IP
	MaxTTL int
}

type Prober interface {
	Probe(ctx context.Context, target Target) ([]model.Hop, error)
}

// PcapProber issues TCP SYN probes with increasing TTL and sniffs replies
// (ICMP Time-Exceeded from intermediate hops, TCP SYN-ACK/RST from the
// destination). Sends go through a raw IPv4 socket so the kernel handles
// ARP/link-layer framing; pcap is only used to receive.
type PcapProber struct {
	InterfaceName string
	Timeout       time.Duration
	DstPort       uint16
	BaseSrcPort   uint16
}

func NewPcapProber(interfaceName string) *PcapProber {
	return &PcapProber{
		InterfaceName: interfaceName,
		Timeout:       5 * time.Second,
		DstPort:       443,
		BaseSrcPort:   33434,
	}
}

type probeReply struct {
	ttl       int
	hopIP     string
	rttMS     float64
	final     bool
	responded bool
}

func (p *PcapProber) Probe(ctx context.Context, target Target) ([]model.Hop, error) {
	if target.IPv4 == nil || target.IPv4.To4() == nil {
		return nil, errors.New("target must contain an IPv4 address")
	}
	maxTTL := target.MaxTTL
	if maxTTL <= 0 {
		maxTTL = 30
	}
	iface, srcIP, err := chooseInterface(p.InterfaceName)
	if err != nil {
		return nil, err
	}
	dstIP := target.IPv4.To4()

	conn, p4, err := openRawSocket()
	if err != nil {
		return nil, fmt.Errorf("open raw socket: %w", err)
	}
	defer conn.Close()

	sniffer, err := pcap.OpenLive(iface.Name, 1600, false, pcap.BlockForever)
	if err != nil {
		return nil, fmt.Errorf("pcap open %s: %w", iface.Name, err)
	}
	defer sniffer.Close()

	// Tight BPF: ICMP (for Time-Exceeded/Unreachable) plus TCP from the
	// destination back to us on our probe port range. cuts out chatter.
	bpf := fmt.Sprintf("(icmp and dst host %s) or (tcp and src host %s and dst host %s)",
		srcIP.String(), dstIP.String(), srcIP.String())
	if err := sniffer.SetBPFFilter(bpf); err != nil {
		return nil, fmt.Errorf("bpf: %w", err)
	}

	// Pre-issue all probes so replies can overlap. Each TTL gets a unique
	// srcPort — the ICMP Time-Exceeded body echoes the first 8 bytes of
	// our TCP header, which is exactly (srcPort | dstPort), and that's
	// how we correlate a reply back to its TTL.
	portToTTL := make(map[uint16]int, maxTTL)
	sendTimes := make(map[int]time.Time, maxTTL)
	for ttl := 1; ttl <= maxTTL; ttl++ {
		port := p.BaseSrcPort + uint16(ttl)
		portToTTL[port] = ttl
	}

	replies := make(chan probeReply, maxTTL*2)
	var wg sync.WaitGroup
	wg.Add(1)
	stopRx := make(chan struct{})
	go func() {
		defer wg.Done()
		src := gopacket.NewPacketSource(sniffer, sniffer.LinkType())
		src.DecodeOptions.Lazy = true
		src.DecodeOptions.NoCopy = true
		for {
			select {
			case <-stopRx:
				return
			case packet, ok := <-src.Packets():
				if !ok {
					return
				}
				if r, matched := parseReply(packet, portToTTL, dstIP, sendTimes); matched {
					replies <- r
				}
			}
		}
	}()

	for ttl := 1; ttl <= maxTTL; ttl++ {
		port := p.BaseSrcPort + uint16(ttl)
		payload, err := buildTCPSegment(srcIP, dstIP, port, p.DstPort)
		if err != nil {
			close(stopRx)
			wg.Wait()
			return nil, err
		}
		if err := p4.SetTTL(int(ttl)); err != nil {
			close(stopRx)
			wg.Wait()
			return nil, fmt.Errorf("set TTL: %w", err)
		}
		sendTimes[ttl] = time.Now()
		if _, err := conn.WriteTo(payload, &net.IPAddr{IP: dstIP}); err != nil {
			close(stopRx)
			wg.Wait()
			return nil, fmt.Errorf("send ttl=%d: %w", ttl, err)
		}
		// small stagger so routers don't rate-limit us
		time.Sleep(20 * time.Millisecond)
	}

	hops := make([]model.Hop, maxTTL)
	for i := range hops {
		hops[i] = model.Hop{TTL: i + 1, Responded: false}
	}
	deadline := time.After(p.Timeout)
	finalTTL := maxTTL
collect:
	for {
		select {
		case <-ctx.Done():
			break collect
		case <-deadline:
			break collect
		case r := <-replies:
			if r.ttl < 1 || r.ttl > maxTTL {
				continue
			}
			if !hops[r.ttl-1].Responded {
				hops[r.ttl-1] = model.Hop{TTL: r.ttl, IP: r.hopIP, RTTMS: r.rttMS, Responded: true}
			}
			if r.final && r.ttl < finalTTL {
				finalTTL = r.ttl
			}
		}
	}
	close(stopRx)
	wg.Wait()
	if finalTTL < len(hops) {
		hops = hops[:finalTTL]
	}
	return hops, nil
}

func openRawSocket() (net.PacketConn, *ipv4.PacketConn, error) {
	c, err := net.ListenPacket("ip4:tcp", "0.0.0.0")
	if err != nil {
		return nil, nil, err
	}
	return c, ipv4.NewPacketConn(c), nil
}

func buildTCPSegment(srcIP, dstIP net.IP, srcPort, dstPort uint16) ([]byte, error) {
	ip := &layers.IPv4{
		Version:  4,
		IHL:      5,
		Protocol: layers.IPProtocolTCP,
		SrcIP:    srcIP.To4(),
		DstIP:    dstIP.To4(),
	}
	tcp := &layers.TCP{
		SrcPort: layers.TCPPort(srcPort),
		DstPort: layers.TCPPort(dstPort),
		SYN:     true,
		Seq:     uint32(time.Now().UnixNano() & 0xffffffff),
		Window:  14600,
	}
	if err := tcp.SetNetworkLayerForChecksum(ip); err != nil {
		return nil, err
	}
	buf := gopacket.NewSerializeBuffer()
	if err := gopacket.SerializeLayers(buf, gopacket.SerializeOptions{FixLengths: true, ComputeChecksums: true}, tcp); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// parseReply pulls one probe reply out of a captured packet and matches it
// back to the TTL that triggered it.
//
// ICMP Time-Exceeded / Destination-Unreachable: the ICMP payload starts with
// the original outbound IP header, followed by at least 8 bytes of the TCP
// header — those 8 bytes are (srcPort | dstPort | seq). We extract the
// original srcPort and look it up in portToTTL.
//
// TCP from the destination: a SYN-ACK or RST back to us on a probe srcPort
// means the final hop replied. Match srcPort (our port) to TTL directly.
func parseReply(packet gopacket.Packet, portToTTL map[uint16]int, dstIP net.IP, sendTimes map[int]time.Time) (probeReply, bool) {
	netLayer := packet.NetworkLayer()
	if netLayer == nil {
		return probeReply{}, false
	}
	hopIP := netLayer.NetworkFlow().Src().String()

	if icmpLayer := packet.Layer(layers.LayerTypeICMPv4); icmpLayer != nil {
		icmp, _ := icmpLayer.(*layers.ICMPv4)
		typeCode := icmp.TypeCode.Type()
		if typeCode != layers.ICMPv4TypeTimeExceeded && typeCode != layers.ICMPv4TypeDestinationUnreachable {
			return probeReply{}, false
		}
		// icmp.Payload = original IP header + first 8 bytes of TCP
		payload := icmp.Payload
		if len(payload) < 20 {
			return probeReply{}, false
		}
		ihl := int(payload[0]&0x0f) * 4
		if len(payload) < ihl+4 {
			return probeReply{}, false
		}
		origSrcPort := binary.BigEndian.Uint16(payload[ihl : ihl+2])
		ttl, ok := portToTTL[origSrcPort]
		if !ok {
			return probeReply{}, false
		}
		rtt := time.Since(sendTimes[ttl])
		final := false
		// destination-unreachable from the target itself = final hop
		if typeCode == layers.ICMPv4TypeDestinationUnreachable && hopIP == dstIP.String() {
			final = true
		}
		return probeReply{ttl: ttl, hopIP: hopIP, rttMS: float64(rtt.Microseconds()) / 1000.0, final: final, responded: true}, true
	}

	if tcpLayer := packet.Layer(layers.LayerTypeTCP); tcpLayer != nil {
		tcp, _ := tcpLayer.(*layers.TCP)
		ttl, ok := portToTTL[uint16(tcp.DstPort)]
		if !ok {
			return probeReply{}, false
		}
		if !(tcp.SYN && tcp.ACK) && !tcp.RST {
			return probeReply{}, false
		}
		if hopIP != dstIP.String() {
			return probeReply{}, false
		}
		rtt := time.Since(sendTimes[ttl])
		return probeReply{ttl: ttl, hopIP: hopIP, rttMS: float64(rtt.Microseconds()) / 1000.0, final: true, responded: true}, true
	}
	return probeReply{}, false
}

func chooseInterface(name string) (net.Interface, net.IP, error) {
	if name != "" {
		iface, err := net.InterfaceByName(name)
		if err != nil {
			return net.Interface{}, nil, err
		}
		ip, err := firstIPv4(*iface)
		return *iface, ip, err
	}
	ifaces, err := net.Interfaces()
	if err != nil {
		return net.Interface{}, nil, err
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		ip, err := firstIPv4(iface)
		if err == nil {
			return iface, ip, nil
		}
	}
	return net.Interface{}, nil, errors.New("no active IPv4 interface found")
}

func firstIPv4(iface net.Interface) (net.IP, error) {
	addrs, err := iface.Addrs()
	if err != nil {
		return nil, err
	}
	for _, addr := range addrs {
		var ip net.IP
		switch v := addr.(type) {
		case *net.IPNet:
			ip = v.IP
		case *net.IPAddr:
			ip = v.IP
		}
		if ip4 := ip.To4(); ip4 != nil {
			return ip4, nil
		}
	}
	return nil, fmt.Errorf("interface %s has no IPv4 address", iface.Name)
}

type MockProber struct {
	Hops []model.Hop
	Err  error
}

func (m MockProber) Probe(ctx context.Context, target Target) ([]model.Hop, error) {
	if m.Err != nil {
		return nil, m.Err
	}
	hops := make([]model.Hop, len(m.Hops))
	copy(hops, m.Hops)
	return hops, nil
}
