package macip

import (
	"net/netip"
	"reflect"
	"testing"
)

func TestParseEntry(t *testing.T) {
	entry, err := ParseEntry(
		".1.3.6.1.2.1.4.22.1.2",
		".1.3.6.1.2.1.4.22.1.2.146.10.255.85.11",
		[]byte{0x78, 0x9a, 0x18, 0x91, 0xa0, 0x21},
	)
	if err != nil {
		t.Fatal(err)
	}
	if entry.IfIndex != 146 || entry.IP.String() != "10.255.85.11" || entry.MAC != "78:9a:18:91:a0:21" {
		t.Fatalf("entry = %+v", entry)
	}
}

func TestCacheReplacesOneRouterAndCombinesRouters(t *testing.T) {
	cache := NewCache()
	cache.Replace("ne8000", []Entry{
		{IP: netip.MustParseAddr("192.0.2.2"), MAC: "00:11:22:33:44:55"},
		{IP: netip.MustParseAddr("192.0.2.1"), MAC: "00:11:22:33:44:55"},
	})
	cache.Replace("asr9901", []Entry{
		{IP: netip.MustParseAddr("192.0.2.2"), MAC: "00:11:22:33:44:55"},
		{IP: netip.MustParseAddr("198.51.100.8"), MAC: "00:11:22:33:44:55"},
	})

	want := []string{"192.0.2.1", "192.0.2.2", "198.51.100.8"}
	if got := cache.Lookup("00:11:22:33:44:55"); !reflect.DeepEqual(got, want) {
		t.Fatalf("Lookup = %v, want %v", got, want)
	}

	cache.Replace("ne8000", nil)
	want = []string{"192.0.2.2", "198.51.100.8"}
	if got := cache.Lookup("00:11:22:33:44:55"); !reflect.DeepEqual(got, want) {
		t.Fatalf("Lookup after replace = %v, want %v", got, want)
	}
}
