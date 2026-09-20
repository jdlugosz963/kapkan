package app

import (
	"net/netip"
	"testing"
	"time"

	"github.com/kapkan-io/kapkan/internal/engine"
	"github.com/kapkan-io/kapkan/internal/mitigate"
)

func TestAttackHistoryRowMapping(t *testing.T) {
	at := time.Date(2026, 6, 13, 12, 0, 0, 0, time.UTC)
	ev := engine.Event{
		Kind:      engine.AttackStarted,
		AttackID:  "attack-1",
		Scope:     engine.ScopeHost,
		Target:    netip.MustParseAddr("203.0.113.20"),
		Group:     "web",
		Direction: engine.DirIncoming,
		Metric:    engine.MetricPPS,
		Rate:      200000,
		Threshold: 80000,
		Rates:     engine.Rates{PPS: 200000, Mbps: 749, FlowsPerSec: 40000},
		PeakRates: engine.Rates{PPS: 250000, Mbps: 800, FlowsPerSec: 45000},
		At:        at,
		Classification: &engine.Classification{
			Type: engine.AttackNTPAmplification, Confidence: 0.9, SrcPort: 123,
		},
		Sample: &engine.AttackSample{
			TopSources:   []engine.Counter{{Key: "198.51.100.7"}, {Key: "198.51.100.8"}},
			TopUpstreams: []engine.Counter{{Key: "Netia", Packets: 60000, Bytes: 1000}},
		},
	}
	ban := &mitigate.Ban{State: mitigate.BanActive, DryRun: true}

	r := attackHistoryStarted(ev, ban)
	if r.StartedAt != "2026-06-13 12:00:00" || r.UpdatedAt != r.StartedAt {
		t.Errorf("started/updated = %q/%q, want ClickHouse UTC literal", r.StartedAt, r.UpdatedAt)
	}
	if r.AttackID != "attack-1" || r.Version != 1 || r.Status != "active" || r.Scope != "host" || r.Target != "203.0.113.20" {
		t.Errorf("history identity/scope/target = %+v", r)
	}
	if r.Group != "web" || r.Direction != "incoming" || r.Metric != "pps" {
		t.Errorf("group/direction/metric = %q/%q/%q", r.Group, r.Direction, r.Metric)
	}
	if r.AttackType != "ntp_amplification" {
		t.Errorf("attack_type = %q, want ntp_amplification", r.AttackType)
	}
	if r.Sample == "" || r.Classification == "" || r.Rates == "" || r.PeakRates == "" {
		t.Errorf("full evidence missing: %+v", r)
	}
	if r.BanState != "active" || r.DryRun != 1 {
		t.Errorf("ban_state/dry_run = %q/%d, want active/1", r.BanState, r.DryRun)
	}
	if r.Rate != 200000 || r.Threshold != 80000 || r.PPS != 200000 || r.FlowsPS != 40000 || r.PeakPPS != 250000 {
		t.Errorf("rate fields = %+v", r)
	}

	ended := attackHistoryEnded(r, engine.Event{AttackID: "attack-1", At: at.Add(time.Minute), Rates: engine.Rates{PPS: 10}, PeakRates: ev.PeakRates}, nil)
	if ended.Version != 2 || ended.Status != "ended" || ended.EndedAt == nil || *ended.EndedAt != "2026-06-13 12:01:00" || ended.Sample != r.Sample {
		t.Errorf("ended row did not retain start evidence: %+v", ended)
	}
}

func TestTrafficRowMapping(t *testing.T) {
	h := engine.HostStat{
		Target:   netip.MustParseAddr("203.0.113.20"),
		Group:    "web",
		Rates:    engine.Rates{PPS: 12000, Mbps: 90, FlowsPerSec: 6000},
		InAttack: true,
		Baseline: &engine.Rates{PPS: 4000},
	}
	r := trafficRow(h, "2026-06-13 12:00:00")
	if r.Scope != "host" || r.Key != "203.0.113.20" || r.Group != "web" {
		t.Errorf("scope/key/group = %q/%q/%q", r.Scope, r.Key, r.Group)
	}
	if r.PPS != 12000 || r.Mbps != 90 || r.FlowsPS != 6000 {
		t.Errorf("rates = %+v", r)
	}
	if r.InAttack != 1 {
		t.Errorf("in_attack = %d, want 1", r.InAttack)
	}
	if r.BaselinePPS != 4000 {
		t.Errorf("baseline_pps = %v, want 4000", r.BaselinePPS)
	}

	// No baseline: field stays zero, no panic.
	r = trafficRow(engine.HostStat{Target: netip.MustParseAddr("203.0.113.21")}, "ts")
	if r.InAttack != 0 || r.BaselinePPS != 0 {
		t.Errorf("quiet host: in_attack/baseline = %d/%v, want 0/0", r.InAttack, r.BaselinePPS)
	}
}
