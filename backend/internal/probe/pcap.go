package probe

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	"github.com/google/gopacket/pcap"
	"github.com/yifans/NetworkPilot/backend/internal/model"
)

const requiredBPFFilter = "icmp or tcp"

type BPFFilterSetter interface {
	SetBPFFilter(expr string) error
}

type Target struct {
	Domain string
	IPv4   net.IP
	MaxTTL int
}

type Prober interface {
	Probe(ctx context.Context, target Target) ([]model.Hop, error)
}

type PcapProber struct {
	InterfaceName string
	Timeout       time.Duration
}

func ApplyRequiredBPF(handle BPFFilterSetter) error {
	return handle.SetBPFFilter(requiredBPFFilter)
}

func NewPcapProber(interfaceName string) *PcapProber {
	return &PcapProber{
		InterfaceName: interfaceName,
		Timeout:       800 * time.Millisecond,
	}
}

func (p *PcapProber) Probe(ctx context.Context, target Target) ([]model.Hop, error) {
	if target.IPv4 == nil || target.IPv4.To4() == nil {
		return nil, errors.New("target must contain an IPv4 address")
	}
	maxTTL := target.MaxTTL
	if maxTTL <= 0 {
		maxTTL = 40
	}
	iface, srcIP, err := chooseInterface(p.InterfaceName)
	if err != nil {
		return nil, err
	}
	handle, err := pcap.OpenLive(iface.Name, 65535, true, pcap.BlockForever)
	if err != nil {
		return nil, err
	}
	defer handle.Close()
	if err := ApplyRequiredBPF(handle); err != nil {
		return nil, err
	}

	packetSource := gopacket.NewPacketSource(handle, handle.LinkType())
	hops := make([]model.Hop, 0, maxTTL)
	for ttl := 1; ttl <= maxTTL; ttl++ {
		select {
		case <-ctx.Done():
			return hops, ctx.Err()
		default:
		}
		srcPort := randomTCPPort()
		payload, err := buildTCPSYNPacket(srcIP, target.IPv4.To4(), srcPort, uint8(ttl))
		if err != nil {
			return hops, err
		}
		start := time.Now()
		if err := handle.WritePacketData(payload); err != nil {
			return hops, err
		}
		hop, complete := waitForHop(ctx, packetSource, ttl, srcPort, time.Since(start), p.timeoutOrDefault())
		hops = append(hops, hop)
		if complete {
			break
		}
	}
	return hops, nil
}

func (p *PcapProber) timeoutOrDefault() time.Duration {
	if p.Timeout <= 0 {
		return 800 * time.Millisecond
	}
	return p.Timeout
}

func buildTCPSYNPacket(srcIP, dstIP net.IP, srcPort layers.TCPPort, ttl uint8) ([]byte, error) {
	ip := &layers.IPv4{
		Version:  4,
		IHL:      5,
		TTL:      ttl,
		Protocol: layers.IPProtocolTCP,
		SrcIP:    srcIP.To4(),
		DstIP:    dstIP.To4(),
	}
	tcp := &layers.TCP{
		SrcPort: srcPort,
		DstPort: 443,
		SYN:     true,
		Seq:     uint32(time.Now().UnixNano()),
		Window:  14600,
	}
	if err := tcp.SetNetworkLayerForChecksum(ip); err != nil {
		return nil, err
	}
	buf := gopacket.NewSerializeBuffer()
	opts := gopacket.SerializeOptions{FixLengths: true, ComputeChecksums: true}
	if err := gopacket.SerializeLayers(buf, opts, ip, tcp); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func waitForHop(ctx context.Context, source *gopacket.PacketSource, ttl int, srcPort layers.TCPPort, elapsed time.Duration, timeout time.Duration) (model.Hop, bool) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return model.Hop{TTL: ttl, Responded: false}, false
		case <-timer.C:
			return model.Hop{TTL: ttl, Responded: false}, false
		case packet := <-source.Packets():
			if packet == nil {
				return model.Hop{TTL: ttl, Responded: false}, false
			}
			if hop, complete, ok := parseProbeReply(packet, ttl, srcPort, elapsed); ok {
				return hop, complete
			}
		}
	}
}

func parseProbeReply(packet gopacket.Packet, ttl int, srcPort layers.TCPPort, elapsed time.Duration) (model.Hop, bool, bool) {
	network := packet.NetworkLayer()
	if network == nil {
		return model.Hop{}, false, false
	}
	srcIP := network.NetworkFlow().Src().String()
	if icmpLayer := packet.Layer(layers.LayerTypeICMPv4); icmpLayer != nil {
		return model.Hop{TTL: ttl, IP: srcIP, RTTMS: float64(elapsed.Microseconds()) / 1000.0, Responded: true}, false, true
	}
	if tcpLayer := packet.Layer(layers.LayerTypeTCP); tcpLayer != nil {
		tcp := tcpLayer.(*layers.TCP)
		if tcp.DstPort == srcPort && (tcp.SYN || tcp.RST || tcp.ACK) {
			return model.Hop{TTL: ttl, IP: srcIP, RTTMS: float64(elapsed.Microseconds()) / 1000.0, Responded: true}, true, true
		}
	}
	return model.Hop{}, false, false
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

func randomTCPPort() layers.TCPPort {
	var b [2]byte
	if _, err := rand.Read(b[:]); err != nil {
		return layers.TCPPort(40000 + time.Now().UnixNano()%20000)
	}
	port := binary.BigEndian.Uint16(b[:])
	return layers.TCPPort(32768 + port%28232)
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
