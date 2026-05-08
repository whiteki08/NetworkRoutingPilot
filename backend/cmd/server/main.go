package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/yifans/NetworkPilot/backend/internal/api"
	"github.com/yifans/NetworkPilot/backend/internal/config"
	"github.com/yifans/NetworkPilot/backend/internal/geoip"
	"github.com/yifans/NetworkPilot/backend/internal/model"
	"github.com/yifans/NetworkPilot/backend/internal/orchestrator"
	"github.com/yifans/NetworkPilot/backend/internal/parser"
	"github.com/yifans/NetworkPilot/backend/internal/probe"
	"github.com/yifans/NetworkPilot/backend/internal/resolver"
	"github.com/yifans/NetworkPilot/backend/internal/ripe"
	"github.com/yifans/NetworkPilot/backend/internal/store"
)

func main() {
	logger := log.New(os.Stdout, "pbr ", log.LstdFlags|log.Lmicroseconds)
	cfg := config.LoadFromEnv()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	dataStore, storeCloser := buildStore(ctx, cfg, logger)
	defer storeCloser()

	enricher, err := geoip.Open(cfg.MMDBCityPath, cfg.MMDBASNPath)
	if err != nil {
		logger.Printf("geoip open failed, continuing without enrichment: %v", err)
		enricher = geoip.NoopEnricher{}
	}
	defer enricher.Close()

	dohResolver := resolver.NewDoHResolver(cfg.DoHEndpoint, cfg.DoHBootstrapIPs)

	var prober probe.Prober
	if strings.EqualFold(cfg.ProbeMode, "mock") {
		logger.Printf("probe mode=mock; using synthetic hops")
		prober = mockProber()
	} else {
		prober = probe.NewPcapProber(cfg.ProbeInterface)
	}

	ripeClient, ripeCloser := buildRIPE(cfg, logger)
	defer ripeCloser()

	orch := &orchestrator.Orchestrator{
		Store:        dataStore,
		Resolver:     dohResolver,
		Prober:       prober,
		Enricher:     enricher,
		Concurrency:  cfg.WorkerConcurrency,
		MaxTTL:       cfg.MaxTTL,
		ProbeTimeout: 60 * time.Second,
		Logger:       logger,
	}

	server := &api.Server{
		Store:        dataStore,
		Orchestrator: orch,
		RIPE:         ripeClient,
		Fetcher:      parser.HTTPFetcher{},
	}

	httpServer := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           api.NewRouter(server),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		logger.Printf("listening on %s (probe_mode=%s)", cfg.HTTPAddr, cfg.ProbeMode)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatalf("http server: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	logger.Println("shutdown signal received")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		logger.Printf("graceful shutdown failed: %v", err)
	}
}

func buildStore(ctx context.Context, cfg config.Config, logger *log.Logger) (store.Store, func()) {
	if cfg.DatabaseURL == "" {
		logger.Println("DATABASE_URL not set; using in-memory store")
		return store.NewMemoryStore(), func() {}
	}
	pg, err := store.NewPostgresStore(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Printf("postgres unavailable (%v); falling back to memory", err)
		return store.NewMemoryStore(), func() {}
	}
	return pg, func() { pg.Close() }
}

func buildRIPE(cfg config.Config, logger *log.Logger) (*ripe.Client, func()) {
	if cfg.RedisURL == "" {
		logger.Println("REDIS_URL not set; RIPE responses cached in memory")
		return ripe.NewClient("", ripe.NewMemoryCache(), cfg.RIPECacheTTL), func() {}
	}
	opts, err := redis.ParseURL(cfg.RedisURL)
	if err != nil {
		logger.Printf("invalid REDIS_URL (%v); falling back to memory cache", err)
		return ripe.NewClient("", ripe.NewMemoryCache(), cfg.RIPECacheTTL), func() {}
	}
	client := redis.NewClient(opts)
	return ripe.NewClient("", ripe.NewRedisCache(client), cfg.RIPECacheTTL), func() { _ = client.Close() }
}

func mockProber() probe.Prober {
	return syntheticProber{}
}

type syntheticProber struct{}

func (syntheticProber) Probe(ctx context.Context, target probe.Target) ([]model.Hop, error) {
	seed := 0
	for _, b := range target.IPv4.To4() {
		seed = seed*31 + int(b)
	}
	bucket := seed % 5
	switch bucket {
	case 0:
		return []model.Hop{
			{TTL: 1, IP: "192.168.1.1", Responded: true, RTTMS: 1.2, CountryCode: "CN", City: "Shanghai", Latitude: 31.23, Longitude: 121.47, ASN: 4134},
			{TTL: 5, IP: "101.95.20.1", Responded: true, RTTMS: 3.5, CountryCode: "CN", City: "Shanghai", Latitude: 31.23, Longitude: 121.47, ASN: 4134},
			{TTL: 9, IP: "59.43.246.101", Responded: true, RTTMS: 140.2, CountryCode: "US", City: "Los Angeles", Latitude: 34.05, Longitude: -118.24, ASN: 4809},
			{TTL: 12, IP: target.IPv4.String(), Responded: true, RTTMS: 155.1, CountryCode: "US", City: "Los Angeles", Latitude: 34.05, Longitude: -118.24, ASN: 4809},
		}, nil
	case 1:
		return []model.Hop{
			{TTL: 1, IP: "10.0.0.1", Responded: true, RTTMS: 0.9, CountryCode: "CN", City: "Beijing", Latitude: 39.9, Longitude: 116.4, ASN: 4134},
			{TTL: 6, IP: "219.158.0.1", Responded: true, RTTMS: 4.1, CountryCode: "CN", City: "Beijing", Latitude: 39.9, Longitude: 116.4, ASN: 4134},
			{TTL: 11, IP: "218.30.1.1", Responded: true, RTTMS: 165.0, CountryCode: "US", City: "San Jose", Latitude: 37.33, Longitude: -121.89, ASN: 4134},
			{TTL: 14, IP: target.IPv4.String(), Responded: true, RTTMS: 170.0, CountryCode: "US", City: "San Jose", Latitude: 37.33, Longitude: -121.89, ASN: 15169},
		}, nil
	case 2:
		return []model.Hop{
			{TTL: 1, IP: "10.0.0.1", Responded: true, RTTMS: 0.7, CountryCode: "CN", City: "Guangzhou", Latitude: 23.13, Longitude: 113.26, ASN: 4538},
			{TTL: 5, IP: "202.97.11.11", Responded: true, RTTMS: 4.5, CountryCode: "CN", City: "Guangzhou", Latitude: 23.13, Longitude: 113.26, ASN: 4538},
			{TTL: 10, IP: "198.51.100.1", Responded: true, RTTMS: 220.0, CountryCode: "US", City: "Seattle", Latitude: 47.61, Longitude: -122.33, ASN: 4538},
			{TTL: 14, IP: target.IPv4.String(), Responded: true, RTTMS: 240.0, CountryCode: "US", City: "Seattle", Latitude: 47.61, Longitude: -122.33, ASN: 4538},
		}, nil
	case 3:
		return []model.Hop{
			{TTL: 1, IP: "10.0.0.1", Responded: true, RTTMS: 0.6, CountryCode: "CN", City: "Shenzhen", Latitude: 22.54, Longitude: 114.06, ASN: 4134},
			{TTL: 7, IP: "61.139.2.1", Responded: true, RTTMS: 5.1, CountryCode: "CN", City: "Shenzhen", Latitude: 22.54, Longitude: 114.06, ASN: 4134},
			{TTL: 9, IP: "", Responded: false},
			{TTL: 16, Responded: false},
		}, nil
	default:
		return []model.Hop{
			{TTL: 1, IP: "10.0.0.1", Responded: true, RTTMS: 1.0, CountryCode: "CN", City: "Hangzhou", Latitude: 30.27, Longitude: 120.15, ASN: 4134},
			{TTL: 6, IP: "202.97.1.1", Responded: true, RTTMS: 5.2, CountryCode: "CN", City: "Hangzhou", Latitude: 30.27, Longitude: 120.15, ASN: 4134},
			{TTL: 12, IP: target.IPv4.String(), Responded: true, RTTMS: 195.0, CountryCode: "US", City: "San Jose", Latitude: 37.33, Longitude: -121.89, ASN: 15169},
		}, nil
	}
}
