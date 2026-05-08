package geoip

import (
	"net"

	"github.com/oschwald/geoip2-golang"
	"github.com/yifans/NetworkPilot/backend/internal/model"
)

type Enricher interface {
	EnrichHop(hop model.Hop) model.Hop
	Close() error
}

type OfflineDatabase struct {
	city *geoip2.Reader
	asn  *geoip2.Reader
}

func Open(cityPath, asnPath string) (Enricher, error) {
	if cityPath == "" && asnPath == "" {
		return NoopEnricher{}, nil
	}
	db := &OfflineDatabase{}
	var err error
	if cityPath != "" {
		db.city, err = geoip2.Open(cityPath)
		if err != nil {
			return nil, err
		}
	}
	if asnPath != "" {
		db.asn, err = geoip2.Open(asnPath)
		if err != nil {
			_ = db.Close()
			return nil, err
		}
	}
	return db, nil
}

func (db *OfflineDatabase) EnrichHop(hop model.Hop) model.Hop {
	ip := net.ParseIP(hop.IP)
	if ip == nil {
		return hop
	}
	if db.city != nil {
		if record, err := db.city.City(ip); err == nil && record != nil {
			hop.CountryCode = record.Country.IsoCode
			hop.City = record.City.Names["en"]
			hop.Latitude = record.Location.Latitude
			hop.Longitude = record.Location.Longitude
		}
	}
	if db.asn != nil {
		if record, err := db.asn.ASN(ip); err == nil && record != nil {
			hop.ASN = record.AutonomousSystemNumber
		}
	}
	return hop
}

func (db *OfflineDatabase) Close() error {
	if db.city != nil {
		_ = db.city.Close()
	}
	if db.asn != nil {
		_ = db.asn.Close()
	}
	return nil
}

type NoopEnricher struct{}

func (NoopEnricher) EnrichHop(hop model.Hop) model.Hop {
	return hop
}

func (NoopEnricher) Close() error {
	return nil
}
