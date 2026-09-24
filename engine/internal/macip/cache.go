// Package macip collects and resolves live MAC-to-IP mappings from routers.
package macip

import (
	"fmt"
	"net"
	"net/netip"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// Entry is one IPv4 ARP-table observation returned by a router.
type Entry struct {
	IfIndex uint32
	IP      netip.Addr
	MAC     string
}

// ParseEntry decodes an ipNetToMediaPhysAddress OID and its octet-string value.
// The instance suffix is ifIndex followed by the four IPv4 octets.
func ParseEntry(baseOID, oid string, value []byte) (Entry, error) {
	base := strings.Trim(strings.TrimSpace(baseOID), ".")
	instance := strings.Trim(strings.TrimSpace(oid), ".")
	prefix := base + "."
	if !strings.HasPrefix(instance, prefix) {
		return Entry{}, fmt.Errorf("OID %q is outside base %q", oid, baseOID)
	}
	parts := strings.Split(strings.TrimPrefix(instance, prefix), ".")
	if len(parts) != 5 {
		return Entry{}, fmt.Errorf("OID %q: expected ifIndex and four IPv4 octets", oid)
	}
	ifIndex, err := strconv.ParseUint(parts[0], 10, 32)
	if err != nil {
		return Entry{}, fmt.Errorf("OID %q: invalid ifIndex: %w", oid, err)
	}
	var octets [4]byte
	for i := range octets {
		value, err := strconv.ParseUint(parts[i+1], 10, 8)
		if err != nil {
			return Entry{}, fmt.Errorf("OID %q: invalid IPv4 octet %q: %w", oid, parts[i+1], err)
		}
		octets[i] = byte(value)
	}
	if len(value) != 6 {
		return Entry{}, fmt.Errorf("OID %q: expected 6-byte MAC, got %d bytes", oid, len(value))
	}
	return Entry{
		IfIndex: uint32(ifIndex),
		IP:      netip.AddrFrom4(octets),
		MAC:     net.HardwareAddr(value).String(),
	}, nil
}

// Cache keeps the latest complete successful snapshot for each router.
type Cache struct {
	mu      sync.RWMutex
	routers map[string]map[string]map[netip.Addr]struct{}
}

func NewCache() *Cache {
	return &Cache{routers: make(map[string]map[string]map[netip.Addr]struct{})}
}

// Retain removes snapshots for routers that are no longer configured.
func (c *Cache) Retain(routers []string) {
	keep := make(map[string]struct{}, len(routers))
	for _, router := range routers {
		keep[router] = struct{}{}
	}
	c.mu.Lock()
	for router := range c.routers {
		if _, ok := keep[router]; !ok {
			delete(c.routers, router)
		}
	}
	c.mu.Unlock()
}

// Replace atomically replaces one router's mappings.
func (c *Cache) Replace(router string, entries []Entry) {
	next := make(map[string]map[netip.Addr]struct{})
	for _, entry := range entries {
		mac := strings.ToLower(entry.MAC)
		if next[mac] == nil {
			next[mac] = make(map[netip.Addr]struct{})
		}
		next[mac][entry.IP] = struct{}{}
	}
	c.mu.Lock()
	c.routers[router] = next
	c.mu.Unlock()
}

// Lookup returns unique, sorted IP addresses observed for a MAC on any router.
func (c *Cache) Lookup(mac string) []string {
	c.mu.RLock()
	unique := make(map[netip.Addr]struct{})
	for _, mappings := range c.routers {
		for ip := range mappings[strings.ToLower(mac)] {
			unique[ip] = struct{}{}
		}
	}
	c.mu.RUnlock()

	ips := make([]netip.Addr, 0, len(unique))
	for ip := range unique {
		ips = append(ips, ip)
	}
	sort.Slice(ips, func(i, j int) bool { return ips[i].Less(ips[j]) })
	out := make([]string, len(ips))
	for i := range ips {
		out[i] = ips[i].String()
	}
	return out
}
