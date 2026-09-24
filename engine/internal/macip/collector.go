package macip

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/gosnmp/gosnmp"
	"github.com/kapkan-io/kapkan/internal/config"
)

type walkTarget struct {
	Address   string
	Community string
	OID       string
	Timeout   time.Duration
	Retries   int
}

type walker interface {
	Walk(context.Context, walkTarget) ([]Entry, error)
}

type snmpWalker struct{}

func (snmpWalker) Walk(ctx context.Context, target walkTarget) ([]Entry, error) {
	client := &gosnmp.GoSNMP{
		Target:         target.Address,
		Port:           161,
		Community:      target.Community,
		Version:        gosnmp.Version2c,
		Context:        ctx,
		Timeout:        target.Timeout,
		Retries:        target.Retries,
		MaxRepetitions: 50,
	}
	if err := client.Connect(); err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	defer func() { _ = client.Conn.Close() }()

	entries := make([]Entry, 0)
	err := client.BulkWalk(target.OID, func(pdu gosnmp.SnmpPDU) error {
		value, ok := pdu.Value.([]byte)
		if !ok {
			return fmt.Errorf("OID %s: expected octet string, got %T", pdu.Name, pdu.Value)
		}
		entry, err := ParseEntry(target.OID, pdu.Name, value)
		if err != nil {
			return err
		}
		entries = append(entries, entry)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("bulk walk %s: %w", target.OID, err)
	}
	return entries, nil
}

// Collector periodically refreshes a shared in-memory cache from all routers.
type Collector struct {
	store  *config.Store
	cache  *Cache
	log    *slog.Logger
	walker walker
}

func NewCollector(store *config.Store, cache *Cache, log *slog.Logger) *Collector {
	return &Collector{
		store: store, cache: cache,
		log: log.With("component", "mac-ip"), walker: snmpWalker{},
	}
}

// Run performs an immediate collection, then follows the configured interval.
// A configuration reload wakes it immediately so router changes do not wait.
func (c *Collector) Run(ctx context.Context) {
	for {
		settings := c.store.Get().MACIPMapping
		c.collect(ctx, settings)
		interval := time.Duration(settings.PollIntervalSeconds) * time.Second
		if interval <= 0 {
			interval = time.Minute
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			stopTimer(timer)
			return
		case <-c.store.Changed():
			stopTimer(timer)
		case <-timer.C:
		}
	}
}

func stopTimer(timer *time.Timer) {
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
}

func (c *Collector) collect(ctx context.Context, settings config.MACIPMapping) {
	if !settings.Enabled {
		c.cache.Retain(nil)
		return
	}
	routerNames := make([]string, len(settings.Routers))
	for i := range settings.Routers {
		routerNames[i] = settings.Routers[i].Name
	}
	c.cache.Retain(routerNames)
	for _, router := range settings.Routers {
		community := os.Getenv(router.CommunityEnv)
		if community == "" {
			c.log.Error("SNMP community environment variable is empty", "router", router.Name, "env", router.CommunityEnv)
			continue
		}
		entries, err := c.walker.Walk(ctx, walkTarget{
			Address: router.Address, Community: community, OID: settings.OID,
			Timeout: time.Duration(settings.TimeoutSeconds) * time.Second, Retries: settings.Retries,
		})
		if err != nil {
			c.log.Warn("SNMP ARP-table read failed; keeping previous snapshot", "router", router.Name, "address", router.Address, "err", err)
			continue
		}
		c.cache.Replace(router.Name, entries)
		c.log.Debug("SNMP ARP-table snapshot refreshed", "router", router.Name, "entries", len(entries))
	}
}
