package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseWithOverlay(t *testing.T) {
	base := []byte(`
dry_run: false
listen:
  sflow: ":6343"
  netflow: ":2055"
sampling:
  default_rate: 1000
networks: ["203.0.113.0/24"]
thresholds: {pps: 100, mbps: 200, flows_per_sec: 300}
ban: {ttl_seconds: 600, max_active_bans: 50, state_file: "/var/lib/kapkan/bans.json"}
bgp:
  local_asn: 65001
  router_id: "10.0.0.1"
  next_hop: "192.0.2.1"
  community: "65000:666"
  neighbors: [{address: "10.0.0.254", remote_asn: 65000}]
storage:
  clickhouse:
    url: "http://127.0.0.1:8123"
    database: "kapkan"
api: {listen: "127.0.0.1:8080"}
`)
	overlay := []byte(`
listen: {netflow: "172.16.7.9:12055"}
api: {listen: "172.16.7.9:18080"}
bgp: {neighbors: [], listen_port: -1}
storage: {clickhouse: {database: "kapkan_dev"}}
ban: {state_file: "/home/serwis/kapkan-dev/bans.json"}
`)

	cfg, err := ParseWithOverlay(base, overlay)
	if err != nil {
		t.Fatalf("ParseWithOverlay: %v", err)
	}
	if cfg.Listen.SFlow != ":6343" || cfg.Listen.NetFlow != "172.16.7.9:12055" {
		t.Errorf("listen = %+v", cfg.Listen)
	}
	if len(cfg.BGP.Neighbors) != 0 || cfg.BGP.ListenPort != -1 {
		t.Errorf("bgp neighbors=%v listen_port=%d", cfg.BGP.Neighbors, cfg.BGP.ListenPort)
	}
	if cfg.Storage.ClickHouse.Database != "kapkan_dev" || cfg.Storage.ClickHouse.URL != "http://127.0.0.1:8123" {
		t.Errorf("clickhouse = %+v", cfg.Storage.ClickHouse)
	}
	if cfg.Ban.StateFile != "/home/serwis/kapkan-dev/bans.json" {
		t.Errorf("ban.state_file = %q", cfg.Ban.StateFile)
	}

	withoutOverlay, err := ParseWithOverlay(base, nil)
	if err != nil {
		t.Fatalf("ParseWithOverlay without overlay: %v", err)
	}
	if len(withoutOverlay.BGP.Neighbors) != 1 || withoutOverlay.Storage.ClickHouse.Database != "kapkan" {
		t.Errorf("empty overlay changed base config")
	}
}

func TestStoreReloadsBaseWithOverlay(t *testing.T) {
	dir := t.TempDir()
	basePath := filepath.Join(dir, "base.yaml")
	overlayPath := filepath.Join(dir, "overlay.yaml")
	base := []byte(`
listen: {netflow: ":2055"}
sampling: {default_rate: 1000}
networks: ["203.0.113.0/24"]
thresholds: {pps: 100, mbps: 200, flows_per_sec: 300}
ban: {ttl_seconds: 600, max_active_bans: 50}
bgp:
  local_asn: 65001
  router_id: "10.0.0.1"
  next_hop: "192.0.2.1"
  community: "65000:666"
api: {listen: "127.0.0.1:8080"}
`)
	if err := os.WriteFile(basePath, base, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(overlayPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadWithOverlay(basePath, overlayPath)
	if err != nil {
		t.Fatalf("empty overlay: %v", err)
	}
	if cfg.Thresholds.PPS != 100 {
		t.Fatalf("empty overlay pps = %d, want 100", cfg.Thresholds.PPS)
	}

	if err := os.WriteFile(overlayPath, []byte("thresholds: {pps: 101}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := NewStoreWithOverlay(basePath, overlayPath, cfg)
	next, err := store.Reload()
	if err != nil {
		t.Fatalf("Reload: %v", err)
	}
	if next.Thresholds.PPS != 101 || next.Thresholds.Mbps != 200 {
		t.Errorf("reloaded thresholds = %+v", next.Thresholds)
	}
}
