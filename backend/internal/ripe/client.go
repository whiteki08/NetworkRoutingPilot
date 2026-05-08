package ripe

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"golang.org/x/sync/singleflight"
)

type Cache interface {
	Get(ctx context.Context, key string) ([]byte, bool, error)
	Set(ctx context.Context, key string, value []byte, ttl time.Duration) error
}

type Record struct {
	Resource     string   `json:"resource"`
	ASN          int      `json:"asn,omitempty"`
	TargetPrefix string   `json:"target_prefix,omitempty"`
	Communities  []string `json:"communities,omitempty"`
	Source       string   `json:"source"`
	Cached       bool     `json:"cached"`
}

type Client struct {
	BaseURL string
	HTTP    *http.Client
	Cache   Cache
	TTL     time.Duration
	group   singleflight.Group
}

func NewClient(baseURL string, cache Cache, ttl time.Duration) *Client {
	if baseURL == "" {
		baseURL = "https://stat.ripe.net/data/bgp-state/data.json"
	}
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	return &Client{
		BaseURL: baseURL,
		HTTP:    &http.Client{Timeout: 8 * time.Second},
		Cache:   cache,
		TTL:     ttl,
	}
}

func (c *Client) Lookup(ctx context.Context, resource string) (Record, error) {
	key := "ripe:bgp-state:" + resource
	if c.Cache != nil {
		if cached, ok, err := c.Cache.Get(ctx, key); err == nil && ok {
			var rec Record
			if err := json.Unmarshal(cached, &rec); err == nil {
				rec.Cached = true
				return rec, nil
			}
		}
	}

	value, err, _ := c.group.Do(key, func() (any, error) {
		if c.Cache != nil {
			if cached, ok, err := c.Cache.Get(ctx, key); err == nil && ok {
				var rec Record
				if err := json.Unmarshal(cached, &rec); err == nil {
					rec.Cached = true
					return rec, nil
				}
			}
		}
		rec, err := c.fetch(ctx, resource)
		if err != nil {
			return Record{}, err
		}
		rec.Cached = false
		if c.Cache != nil {
			encoded, _ := json.Marshal(rec)
			_ = c.Cache.Set(ctx, key, encoded, c.TTL)
		}
		return rec, nil
	})
	if err != nil {
		return Record{}, err
	}
	return value.(Record), nil
}

func (c *Client) fetch(ctx context.Context, resource string) (Record, error) {
	u, err := url.Parse(c.BaseURL)
	if err != nil {
		return Record{}, err
	}
	q := u.Query()
	q.Set("resource", resource)
	u.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return Record{}, err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return Record{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Record{}, fmt.Errorf("ripe returned status %d", resp.StatusCode)
	}
	var payload map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return Record{}, err
	}
	return parseRecord(resource, payload), nil
}

func parseRecord(resource string, payload map[string]any) Record {
	rec := Record{Resource: resource, Source: "ripe"}
	data, _ := payload["data"].(map[string]any)
	if data == nil {
		return rec
	}
	if arr, _ := data["bgp_state"].([]any); len(arr) > 0 {
		if first, _ := arr[0].(map[string]any); first != nil {
			if prefix, _ := first["target_prefix"].(string); prefix != "" {
				rec.TargetPrefix = prefix
			}
			if asn, ok := first["asn"].(float64); ok {
				rec.ASN = int(asn)
			}
			if communities, _ := first["community"].([]any); len(communities) > 0 {
				for _, community := range communities {
					if s, ok := community.(string); ok {
						rec.Communities = append(rec.Communities, s)
					}
				}
			}
		}
	}
	if rec.TargetPrefix == "" {
		if prefix, _ := data["resource"].(string); prefix != "" {
			rec.TargetPrefix = prefix
		}
	}
	return rec
}

type MemoryCache struct {
	mu    sync.Mutex
	items map[string]cacheItem
}

type cacheItem struct {
	value     []byte
	expiresAt time.Time
}

func NewMemoryCache() *MemoryCache {
	return &MemoryCache{items: make(map[string]cacheItem)}
}

func (m *MemoryCache) Get(ctx context.Context, key string) ([]byte, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	item, ok := m.items[key]
	if !ok || time.Now().After(item.expiresAt) {
		delete(m.items, key)
		return nil, false, nil
	}
	value := make([]byte, len(item.value))
	copy(value, item.value)
	return value, true, nil
}

func (m *MemoryCache) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	copied := make([]byte, len(value))
	copy(copied, value)
	m.items[key] = cacheItem{value: copied, expiresAt: time.Now().Add(ttl)}
	return nil
}

type RedisCache struct {
	client *redis.Client
}

func NewRedisCache(client *redis.Client) *RedisCache {
	return &RedisCache{client: client}
}

func (r *RedisCache) Get(ctx context.Context, key string) ([]byte, bool, error) {
	value, err := r.client.Get(ctx, key).Bytes()
	if err == redis.Nil {
		return nil, false, nil
	}
	return value, err == nil, err
}

func (r *RedisCache) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	return r.client.Set(ctx, key, value, ttl).Err()
}
