package parser

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type Fetcher interface {
	Fetch(ctx context.Context, resource string) (io.ReadCloser, error)
}

type HTTPFetcher struct {
	BaseDir string
	Client  *http.Client
}

func (f HTTPFetcher) Fetch(ctx context.Context, resource string) (io.ReadCloser, error) {
	if strings.HasPrefix(resource, "http://") || strings.HasPrefix(resource, "https://") {
		client := f.Client
		if client == nil {
			client = &http.Client{Timeout: 15 * time.Second}
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, resource, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", "NetworkPilot/1.0")
		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			_ = resp.Body.Close()
			return nil, fmt.Errorf("resource %s returned status %d", resource, resp.StatusCode)
		}
		return resp.Body, nil
	}
	return os.Open(filepath.Join(f.BaseDir, resource))
}

func ParseSurgeDomains(ctx context.Context, reader io.Reader, fetcher Fetcher) ([]string, error) {
	domains := make(map[string]bool)
	if err := parseRules(ctx, reader, fetcher, false, domains); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(domains))
	for domain := range domains {
		out = append(out, domain)
	}
	sort.Strings(out)
	return out, nil
}

func parseRules(ctx context.Context, reader io.Reader, fetcher Fetcher, domainSet bool, domains map[string]bool) error {
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		line := cleanLine(scanner.Text())
		if line == "" {
			continue
		}
		parts := strings.Split(line, ",")
		if domainSet && len(parts) == 1 {
			addDomain(domains, parts[0])
			continue
		}
		if len(parts) < 2 {
			continue
		}
		ruleType := strings.ToUpper(strings.TrimSpace(parts[0]))
		if len(parts) >= 3 && ignoredPolicy(parts[2]) {
			continue
		}
		switch ruleType {
		case "DOMAIN", "DOMAIN-SUFFIX":
			addDomain(domains, parts[1])
		case "RULE-SET", "DOMAIN-SET":
			if fetcher == nil {
				continue
			}
			body, err := fetcher.Fetch(ctx, strings.TrimSpace(parts[1]))
			if err != nil {
				continue
			}
			err = parseRules(ctx, body, fetcher, ruleType == "DOMAIN-SET", domains)
			_ = body.Close()
			if err != nil {
				return err
			}
		}
	}
	return scanner.Err()
}

func cleanLine(line string) string {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") || strings.HasPrefix(line, ";") {
		return ""
	}
	if idx := strings.Index(line, " //"); idx >= 0 {
		line = strings.TrimSpace(line[:idx])
	}
	return line
}

func addDomain(domains map[string]bool, value string) {
	domain := strings.TrimSpace(strings.ToLower(value))
	domain = strings.TrimPrefix(domain, ".")
	if domain != "" {
		domains[domain] = true
	}
}

func ignoredPolicy(policy string) bool {
	policy = strings.ToUpper(strings.TrimSpace(strings.Split(policy, "//")[0]))
	switch policy {
	case "DIRECT", "REJECT", "REJECT-TINYGIF", "REJECT-DROP", "REJECT-NO-DROP":
		return true
	default:
		return false
	}
}
