package engine

import (
	"net/netip"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/kapkan-io/kapkan/internal/config"
	"github.com/kapkan-io/kapkan/internal/flow"
)

type fakeMACIPResolver map[string][]string

func (r fakeMACIPResolver) Lookup(mac string) []string { return r[mac] }

func TestOpenPeeringSnapshot(t *testing.T) {
	yaml := strings.Replace(baseYAML, "networks:", `attribution:
  mode: vlan
  vlans:
    - {exporter: "198.51.100.9", vlan: 992, name: "EPIX"}
open_peering:
  members:
    - {exporter: "198.51.100.9", vlan: 992}
networks:`, 1)
	cfg, err := config.Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	clock := newMockClock()
	eng := New(config.NewStore("", cfg), WithClock(clock.Now), WithWindow(5), WithMACIPResolver(fakeMACIPResolver{
		"00:59:dc:16:5c:e9": {"10.255.85.11"},
	}))
	exporter := netip.MustParseAddr("198.51.100.9")
	peer := [6]byte{0x00, 0x59, 0xdc, 0x16, 0x5c, 0xe9}
	eng.Process(flow.Flow{
		SrcAddr: netip.MustParseAddr("192.0.2.1"), DstAddr: netip.MustParseAddr("203.0.113.2"),
		Exporter: exporter, SrcVLAN: 992, SrcMAC: peer,
		Bytes: 1_000_000, Packets: 1000, SamplingRate: 10,
	})
	eng.Process(flow.Flow{
		SrcAddr: netip.MustParseAddr("203.0.113.2"), DstAddr: netip.MustParseAddr("192.0.2.1"),
		Exporter: exporter, SrcVLAN: 992, DstMAC: peer,
		Bytes: 500_000, Packets: 500, SamplingRate: 10,
	})
	clock.Advance(time.Second)

	snapshot := eng.OpenPeeringSnapshot()
	if !snapshot.Enabled || snapshot.Ingress.Mbps != 16 || snapshot.Egress.Mbps != 8 {
		t.Fatalf("snapshot = %+v, want enabled ingress=16Mbps egress=8Mbps", snapshot)
	}
	if len(snapshot.Ingress.TopMACs) != 1 || snapshot.Ingress.TopMACs[0].MAC != "00:59:dc:16:5c:e9" {
		t.Fatalf("ingress top MACs = %+v", snapshot.Ingress.TopMACs)
	}
	if len(snapshot.Egress.TopMACs) != 1 || snapshot.Egress.TopMACs[0].Share != 1 {
		t.Fatalf("egress top MACs = %+v", snapshot.Egress.TopMACs)
	}
	if len(snapshot.Ingress.Members) != 1 || snapshot.Ingress.Members[0].Name != "EPIX" || snapshot.Ingress.Members[0].Mbps != 16 {
		t.Fatalf("ingress members = %+v", snapshot.Ingress.Members)
	}
	if len(snapshot.Ingress.AllMACs) != 1 {
		t.Fatalf("ingress all MACs = %+v", snapshot.Ingress.AllMACs)
	}
	if got := snapshot.Ingress.Members[0].TopMACs[0].IPs; !reflect.DeepEqual(got, []string{"10.255.85.11"}) {
		t.Fatalf("member IPs = %v", got)
	}
}

func TestOpenPeeringUnknownMACQuality(t *testing.T) {
	acc := openPeeringAccumulator{ring: make([]openPeeringBucket, 2)}
	acc.record(dirIn, 10, "EPIX", [6]byte{}, 100, 2, 5)
	eng := &Engine{windowSec: 1, ringSize: 2, openPeering: acc}
	direction := eng.openPeeringDirection(11, dirIn)
	if direction.Quality != "unavailable" || direction.UnknownShare != 1 || direction.UnknownPPS != 10 {
		t.Fatalf("direction = %+v", direction)
	}
}
