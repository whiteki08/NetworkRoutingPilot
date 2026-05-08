package probe

import (
	"context"
	"net"

	"github.com/yifans/NetworkPilot/backend/internal/model"
)

type Target struct {
	Domain string
	IPv4   net.IP
	MaxTTL int
}

type Prober interface {
	Probe(ctx context.Context, target Target) ([]model.Hop, error)
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
