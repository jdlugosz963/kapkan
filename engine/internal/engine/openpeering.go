package engine

import (
	"net"
	"sort"
	"sync"
)

const maxOpenPeeringMACsPerBucket = 4096

type openPeeringCounter struct {
	trafficCounter
	members map[string]*trafficCounter
}

type trafficCounter struct {
	bytes, packets               uint64
	unknownBytes, unknownPackets uint64
	macs                         map[[6]byte]openPeeringMACCounter
}

type openPeeringMACCounter struct {
	bytes, packets uint64
}

type openPeeringBucket struct {
	epoch int64
	dirs  [2]openPeeringCounter
}

type openPeeringAccumulator struct {
	mu   sync.Mutex
	ring []openPeeringBucket
}

type OpenPeeringSnapshot struct {
	Enabled       bool                 `json:"enabled"`
	WindowSeconds int64                `json:"window_seconds"`
	Ingress       OpenPeeringDirection `json:"ingress"`
	Egress        OpenPeeringDirection `json:"egress"`
}

type OpenPeeringDirection struct {
	Mbps         float64                     `json:"mbps"`
	PPS          float64                     `json:"pps"`
	UnknownMbps  float64                     `json:"unknown_mbps"`
	UnknownPPS   float64                     `json:"unknown_pps"`
	UnknownShare float64                     `json:"unknown_share"`
	DistinctMACs int                         `json:"distinct_macs"`
	Quality      string                      `json:"quality"`
	TopMACs      []OpenPeeringMAC            `json:"top_macs"`
	AllMACs      []OpenPeeringMAC            `json:"all_macs"`
	Members      []OpenPeeringMemberSnapshot `json:"members"`
}

type OpenPeeringMemberSnapshot struct {
	Name         string           `json:"name"`
	Mbps         float64          `json:"mbps"`
	PPS          float64          `json:"pps"`
	UnknownShare float64          `json:"unknown_share"`
	DistinctMACs int              `json:"distinct_macs"`
	Quality      string           `json:"quality"`
	TopMACs      []OpenPeeringMAC `json:"top_macs"`
}

type OpenPeeringMAC struct {
	MAC   string   `json:"mac"`
	IPs   []string `json:"ips,omitempty"`
	Mbps  float64  `json:"mbps"`
	PPS   float64  `json:"pps"`
	Share float64  `json:"share"`
}

// MACIPResolver provides current IP addresses without coupling the engine to
// the SNMP collector that owns them.
type MACIPResolver interface {
	Lookup(mac string) []string
}

func (a *openPeeringAccumulator) record(dir int, epoch int64, member string, mac [6]byte, bytes, packets, rate uint64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	bucket := &a.ring[epoch%int64(len(a.ring))]
	if bucket.epoch != epoch {
		bucket.epoch = epoch
		bucket.dirs = [2]openPeeringCounter{}
	}
	counter := &bucket.dirs[dir]
	correctedBytes := bytes * rate
	correctedPackets := packets * rate
	counter.add(mac, correctedBytes, correctedPackets)
	if counter.members == nil {
		counter.members = make(map[string]*trafficCounter)
	}
	memberCounter := counter.members[member]
	if memberCounter == nil {
		memberCounter = &trafficCounter{}
		counter.members[member] = memberCounter
	}
	memberCounter.add(mac, correctedBytes, correctedPackets)
}

func (counter *trafficCounter) add(mac [6]byte, correctedBytes, correctedPackets uint64) {
	counter.bytes += correctedBytes
	counter.packets += correctedPackets
	if !usablePeerMAC(mac) {
		counter.unknownBytes += correctedBytes
		counter.unknownPackets += correctedPackets
		return
	}
	if counter.macs == nil {
		counter.macs = make(map[[6]byte]openPeeringMACCounter)
	}
	current, found := counter.macs[mac]
	if !found && len(counter.macs) >= maxOpenPeeringMACsPerBucket {
		counter.unknownBytes += correctedBytes
		counter.unknownPackets += correctedPackets
		return
	}
	current.bytes += correctedBytes
	current.packets += correctedPackets
	counter.macs[mac] = current
}

func usablePeerMAC(mac [6]byte) bool {
	if mac == ([6]byte{}) {
		return false
	}
	return mac[0]&1 == 0
}

func (e *Engine) OpenPeeringSnapshot() OpenPeeringSnapshot {
	cfg := e.store.Get()
	snapshot := OpenPeeringSnapshot{
		Enabled: cfg.OpenPeeringEnabled(), WindowSeconds: e.windowSec,
		Ingress: emptyOpenPeeringDirection(), Egress: emptyOpenPeeringDirection(),
	}
	if !snapshot.Enabled {
		return snapshot
	}
	e.openPeering.mu.Lock()
	defer e.openPeering.mu.Unlock()
	nowSec := e.now().Unix()
	snapshot.Ingress = e.openPeeringDirection(nowSec, dirIn)
	snapshot.Egress = e.openPeeringDirection(nowSec, dirOut)
	e.enrichOpenPeeringDirection(&snapshot.Ingress)
	e.enrichOpenPeeringDirection(&snapshot.Egress)
	return snapshot
}

func (e *Engine) enrichOpenPeeringDirection(direction *OpenPeeringDirection) {
	if e.macIP == nil {
		return
	}
	enrich := func(rows []OpenPeeringMAC) {
		for i := range rows {
			rows[i].IPs = e.macIP.Lookup(rows[i].MAC)
		}
	}
	enrich(direction.TopMACs)
	enrich(direction.AllMACs)
	for i := range direction.Members {
		enrich(direction.Members[i].TopMACs)
	}
}

func emptyOpenPeeringDirection() OpenPeeringDirection {
	return OpenPeeringDirection{Quality: "unavailable", TopMACs: []OpenPeeringMAC{}, AllMACs: []OpenPeeringMAC{}, Members: []OpenPeeringMemberSnapshot{}}
}

func (e *Engine) openPeeringDirection(nowSec int64, dir int) OpenPeeringDirection {
	total := trafficCounter{macs: make(map[[6]byte]openPeeringMACCounter)}
	members := make(map[string]*trafficCounter)
	for second := nowSec - e.windowSec; second <= nowSec-1; second++ {
		bucket := &e.openPeering.ring[second%int64(len(e.openPeering.ring))]
		if bucket.epoch != second {
			continue
		}
		current := &bucket.dirs[dir]
		mergeTrafficCounter(&total, &current.trafficCounter)
		for name, member := range current.members {
			combined := members[name]
			if combined == nil {
				combined = &trafficCounter{macs: make(map[[6]byte]openPeeringMACCounter)}
				members[name] = combined
			}
			mergeTrafficCounter(combined, member)
		}
	}
	out := openPeeringDirectionFromCounter(total, float64(e.windowSec), 20)
	out.AllMACs = append([]OpenPeeringMAC(nil), out.TopMACs...)
	allMACs := sortedOpenPeeringMACs(total, float64(e.windowSec))
	out.AllMACs = allMACs
	out.Members = make([]OpenPeeringMemberSnapshot, 0, len(members))
	for name, member := range members {
		direction := openPeeringDirectionFromCounter(*member, float64(e.windowSec), 10)
		out.Members = append(out.Members, OpenPeeringMemberSnapshot{
			Name: name, Mbps: direction.Mbps, PPS: direction.PPS,
			UnknownShare: direction.UnknownShare, DistinctMACs: direction.DistinctMACs,
			Quality: direction.Quality, TopMACs: direction.TopMACs,
		})
	}
	sort.Slice(out.Members, func(i, j int) bool {
		if out.Members[i].Mbps == out.Members[j].Mbps {
			return out.Members[i].Name < out.Members[j].Name
		}
		return out.Members[i].Mbps > out.Members[j].Mbps
	})
	return out
}

func mergeTrafficCounter(dst, src *trafficCounter) {
	dst.bytes += src.bytes
	dst.packets += src.packets
	dst.unknownBytes += src.unknownBytes
	dst.unknownPackets += src.unknownPackets
	for mac, value := range src.macs {
		combined := dst.macs[mac]
		combined.bytes += value.bytes
		combined.packets += value.packets
		dst.macs[mac] = combined
	}
}

func openPeeringDirectionFromCounter(total trafficCounter, window float64, limit int) OpenPeeringDirection {
	out := OpenPeeringDirection{
		Mbps:         float64(total.bytes) * 8 / 1e6 / window,
		PPS:          float64(total.packets) / window,
		UnknownMbps:  float64(total.unknownBytes) * 8 / 1e6 / window,
		UnknownPPS:   float64(total.unknownPackets) / window,
		DistinctMACs: len(total.macs), Quality: "ok",
		TopMACs: []OpenPeeringMAC{}, AllMACs: []OpenPeeringMAC{}, Members: []OpenPeeringMemberSnapshot{},
	}
	if total.bytes > 0 {
		out.UnknownShare = float64(total.unknownBytes) / float64(total.bytes)
	}
	if len(total.macs) == 0 {
		out.Quality = "unavailable"
	} else if total.unknownBytes > 0 {
		out.Quality = "partial"
	}
	out.TopMACs = sortedOpenPeeringMACs(total, window)
	if len(out.TopMACs) > limit {
		out.TopMACs = out.TopMACs[:limit]
	}
	return out
}

func sortedOpenPeeringMACs(total trafficCounter, window float64) []OpenPeeringMAC {
	macs := make([]OpenPeeringMAC, 0, len(total.macs))
	for mac, value := range total.macs {
		share := 0.0
		if total.bytes > 0 {
			share = float64(value.bytes) / float64(total.bytes)
		}
		macs = append(macs, OpenPeeringMAC{
			MAC:   net.HardwareAddr(mac[:]).String(),
			Mbps:  float64(value.bytes) * 8 / 1e6 / window,
			PPS:   float64(value.packets) / window,
			Share: share,
		})
	}
	sort.Slice(macs, func(i, j int) bool {
		if macs[i].Mbps == macs[j].Mbps {
			return macs[i].MAC < macs[j].MAC
		}
		return macs[i].Mbps > macs[j].Mbps
	})
	return macs
}
