package resolver

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

type IPv4Resolver interface {
	LookupIPv4(ctx context.Context, domain string) (net.IP, error)
}

type DoHResolver struct {
	endpoint     string
	client       *http.Client
	cacheTTL     time.Duration
	mu           sync.Mutex
	cache        map[string]cacheEntry
}

type cacheEntry struct {
	ip        net.IP
	expiresAt time.Time
}

type dohResponse struct {
	Status int `json:"Status"`
	Answer []struct {
		Type int    `json:"type"`
		Data string `json:"data"`
	} `json:"Answer"`
}

func NewDoHResolver(endpoint string, bootstrapIPs []string) *DoHResolver {
	if endpoint == "" {
		endpoint = "https://cloudflare-dns.com/dns-query"
	}
	// DOH_INSECURE=true skips TLS verification — useful for private DoH servers
	// whose cert doesn't match their hostname (e.g. wildcard on a different domain).
	insecure := strings.EqualFold(strings.TrimSpace(os.Getenv("DOH_INSECURE")), "true")
	return &DoHResolver{
		endpoint: endpoint,
		client: &http.Client{
			Timeout:   8 * time.Second,
			Transport: bootstrapTransport(endpoint, bootstrapIPs, insecure),
		},
		cacheTTL: 10 * time.Minute,
		cache:    make(map[string]cacheEntry),
	}
}

func (r *DoHResolver) LookupIPv4(ctx context.Context, domain string) (net.IP, error) {
	if ip, ok := r.fromCache(domain); ok {
		return ip, nil
	}
	u, err := url.Parse(r.endpoint)
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("name", domain)
	q.Set("type", "A")
	u.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/dns-json")
	resp, err := r.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("doh returned status %d", resp.StatusCode)
	}
	var payload dohResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}
	for _, answer := range payload.Answer {
		if answer.Type != 1 {
			continue
		}
		ip := net.ParseIP(answer.Data)
		if ip4 := ip.To4(); ip4 != nil {
			r.saveCache(domain, ip4)
			return ip4, nil
		}
	}
	return nil, fmt.Errorf("no IPv4 A record for %s", domain)
}

func (r *DoHResolver) fromCache(domain string) (net.IP, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item, ok := r.cache[domain]
	if !ok || time.Now().After(item.expiresAt) {
		delete(r.cache, domain)
		return nil, false
	}
	return append(net.IP(nil), item.ip...), true
}

func (r *DoHResolver) saveCache(domain string, ip net.IP) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cache[domain] = cacheEntry{ip: append(net.IP(nil), ip...), expiresAt: time.Now().Add(r.cacheTTL)}
}

func bootstrapTransport(endpoint string, bootstrapIPs []string, insecure bool) http.RoundTripper {
	parsed, err := url.Parse(endpoint)
	if err != nil || len(bootstrapIPs) == 0 {
		return http.DefaultTransport
	}
	host := parsed.Hostname()
	dialer := &net.Dialer{Timeout: 6 * time.Second, KeepAlive: 30 * time.Second}
	t := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		TLSClientConfig: &tls.Config{
			MinVersion:         tls.VersionTLS12,
			InsecureSkipVerify: insecure, //nolint:gosec // opt-in for private DoH servers with mismatched certs
		},
	}
	// DialTLSContext lets us set ServerName per-connection: when we dial by
	// bootstrap IP the cert may only have IP SANs (no hostname SAN), so we
	// tell Go to verify against the IP string rather than the URL hostname.
	t.DialTLSContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		reqHost, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		dialAddr := address
		serverName := reqHost
		if reqHost == host {
			ip := bootstrapIPs[rand.Intn(len(bootstrapIPs))]
			dialAddr = net.JoinHostPort(ip, port)
			serverName = ip // verify IP SAN, not hostname
		}
		tlsCfg := t.TLSClientConfig.Clone()
		tlsCfg.ServerName = serverName
		conn, err := dialer.DialContext(ctx, network, dialAddr)
		if err != nil {
			return nil, err
		}
		tlsConn := tls.Client(conn, tlsCfg)
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			conn.Close()
			return nil, err
		}
		return tlsConn, nil
	}
	return t
}

type StaticResolver struct {
	IP  net.IP
	Err error
}

func (s StaticResolver) LookupIPv4(ctx context.Context, domain string) (net.IP, error) {
	if s.Err != nil {
		return nil, s.Err
	}
	return append(net.IP(nil), s.IP...), nil
}
