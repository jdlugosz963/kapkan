package macip

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/netip"
	"reflect"
	"testing"

	"github.com/kapkan-io/kapkan/internal/config"
)

type fakeWalker struct {
	entries []Entry
	err     error
	targets []walkTarget
}

func (w *fakeWalker) Walk(_ context.Context, target walkTarget) ([]Entry, error) {
	w.targets = append(w.targets, target)
	return w.entries, w.err
}

func TestCollectorUsesEnvironmentAndPreservesFailedSnapshot(t *testing.T) {
	t.Setenv("TEST_SNMP_COMMUNITY", "secret")
	settings := config.MACIPMapping{
		Enabled: true, PollIntervalSeconds: 60, OID: ".1.3.6.1.2.1.4.22.1.2",
		TimeoutSeconds: 3, Retries: 1,
		Routers: []config.SNMPRouter{{Name: "edge", Address: "192.0.2.1", CommunityEnv: "TEST_SNMP_COMMUNITY"}},
	}
	cache := NewCache()
	walker := &fakeWalker{entries: []Entry{{IP: netip.MustParseAddr("10.0.0.1"), MAC: "00:11:22:33:44:55"}}}
	collector := &Collector{cache: cache, walker: walker, log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	collector.collect(context.Background(), settings)

	if len(walker.targets) != 1 || walker.targets[0].Community != "secret" {
		t.Fatalf("targets = %+v", walker.targets)
	}
	if got := cache.Lookup("00:11:22:33:44:55"); !reflect.DeepEqual(got, []string{"10.0.0.1"}) {
		t.Fatalf("Lookup = %v", got)
	}

	walker.err = errors.New("timeout")
	walker.entries = nil
	collector.collect(context.Background(), settings)
	if got := cache.Lookup("00:11:22:33:44:55"); !reflect.DeepEqual(got, []string{"10.0.0.1"}) {
		t.Fatalf("Lookup after failure = %v", got)
	}
}
