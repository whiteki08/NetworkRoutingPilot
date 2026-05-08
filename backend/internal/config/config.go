package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	HTTPAddr          string
	DatabaseURL       string
	RedisURL          string
	DoHEndpoint       string
	DoHBootstrapIPs   []string
	MMDBCityPath      string
	MMDBASNPath       string
	ProbeInterface    string
	ProbeMode         string
	MaxTTL            int
	WorkerConcurrency int
	RIPECacheTTL      time.Duration
}

func LoadFromEnv() Config {
	return Config{
		HTTPAddr:          getenv("HTTP_ADDR", ":8080"),
		DatabaseURL:       os.Getenv("DATABASE_URL"),
		RedisURL:          os.Getenv("REDIS_URL"),
		DoHEndpoint:       getenv("DOH_ENDPOINT", "https://cloudflare-dns.com/dns-query"),
		DoHBootstrapIPs:   splitCSV(getenv("DOH_BOOTSTRAP_IPS", "1.1.1.1,1.0.0.1")),
		MMDBCityPath:      os.Getenv("MMDB_CITY_PATH"),
		MMDBASNPath:       os.Getenv("MMDB_ASN_PATH"),
		ProbeInterface:    os.Getenv("PROBE_INTERFACE"),
		ProbeMode:         getenv("PROBE_MODE", "pcap"),
		MaxTTL:            getenvInt("MAX_TTL", 40),
		WorkerConcurrency: getenvInt("WORKER_CONCURRENCY", 100),
		RIPECacheTTL:      time.Duration(getenvInt("RIPE_CACHE_TTL_HOURS", 24)) * time.Hour,
	}
}

func getenv(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

func getenvInt(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

func splitCSV(value string) []string {
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}
