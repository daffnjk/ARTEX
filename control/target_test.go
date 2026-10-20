package control

import (
	"net/netip"
	"testing"
)

func TestControlExactHostBoundary(t *testing.T) {
	rules := []ScopeRule{{Kind: "exact_host", Value: "TEST.INVALID.", Ports: []int{80}}}
	for _, tc := range []struct {
		url string
		ok  bool
	}{{"http://test.invalid/", true}, {"http://TEST.INVALID.:80/", true}, {"http://x.test.invalid/", false}, {"http://eviltest.invalid/", false}, {"http://test.invalid:81/", false}} {
		target, err := NormalizeTarget(tc.url)
		if err != nil {
			t.Fatal(err)
		}
		if got := MatchTarget(rules, target) == nil; got != tc.ok {
			t.Errorf("%s allowed=%v want=%v", tc.url, got, tc.ok)
		}
	}
	sub := []ScopeRule{{Kind: "domain_subtree", Value: "test.invalid", Ports: []int{80}}}
	target, _ := NormalizeTarget("http://x.test.invalid/")
	if err := MatchTarget(sub, target); err != nil {
		t.Fatal(err)
	}
	target, _ = NormalizeTarget("http://eviltest.invalid/")
	if MatchTarget(sub, target) == nil {
		t.Fatal("label boundary escaped")
	}
}
func TestControlDenyWins(t *testing.T) {
	rules := []ScopeRule{{Kind: "domain_subtree", Value: "test.invalid", Ports: []int{80}, Deny: true}, {Kind: "exact_host", Value: "ok.test.invalid", Ports: []int{80}}, {Kind: "cidr", Value: "127.0.0.0/8", Ports: []int{80}}}
	target, _ := NormalizeTarget("http://ok.test.invalid/")
	if MatchTarget(rules, target) == nil {
		t.Fatal("specific allow overrode deny")
	}
	target, _ = NormalizeTarget("http://unlisted.invalid/")
	if MatchTarget(rules, target) == nil {
		t.Fatal("default allow")
	}
	target, _ = NormalizeTarget("http://test.invalid/")
	logical := []ScopeRule{{Kind: "exact_host", Value: "test.invalid", Ports: []int{80}}, {Kind: "cidr", Value: "127.0.0.0/8", Ports: []int{80}}}
	if err := CheckResolved(logical, target, []netip.Addr{netip.MustParseAddr("127.0.0.1")}); err != nil {
		t.Fatal(err)
	}
	if CheckResolved(logical, target, []netip.Addr{netip.MustParseAddr("127.0.0.1"), netip.MustParseAddr("192.0.2.10")}) == nil {
		t.Fatal("mixed DNS answer accepted")
	}
	if CheckResolved(logical, target, nil) == nil {
		t.Fatal("empty DNS answer accepted")
	}
	target, _ = NormalizeTarget("http://other.invalid/")
	if CheckResolved(logical, target, []netip.Addr{netip.MustParseAddr("127.0.0.1")}) == nil {
		t.Fatal("same IP authorized different logical host")
	}
}
func TestControlAmbiguousTargetRejected(t *testing.T) {
	for _, s := range []string{"http://user@localhost/", "ftp://localhost/", "http://localhost:0/", "http://localhost:65536/", "http://localhost:080/", "http://localhost:/", "http://127.1/", "http://0177.0.0.1/", "http://2130706433/", "http://0x7f000001/", "http://[fe80::1%25en0]/", "http://localhost/%2e%2e/", "http://localhost/../secret", "http://localhost/#fragment", "http://localhost/?next=x", "http://本地.test/", " http://localhost/", "http://localhost\\evil/"} {
		if _, err := NormalizeTarget(s); err == nil {
			t.Errorf("accepted ambiguous or unsupported %s", s)
		}
	}
	for _, s := range []string{"http://localhost/fixture", "https://[::1]:443/a", "http://127.0.0.1:8080/a", "http://xn--fiqs8s.test/"} {
		if _, err := NormalizeTarget(s); err != nil {
			t.Errorf("rejected supported %s: %v", s, err)
		}
	}
}
func TestControlUnsupportedScopeKind(t *testing.T) {
	target, _ := NormalizeTarget("http://test.invalid/")
	for _, kind := range []string{"url_path", "resource", "keyword", "company", ""} {
		if MatchTarget([]ScopeRule{{Kind: kind, Value: "test.invalid", Ports: []int{80}}}, target) == nil {
			t.Fatal("unsupported constraint widened", kind)
		}
	}
	if MatchTarget([]ScopeRule{{Kind: "exact_host", Value: "test.invalid", Ports: []int{80}}, {Kind: "resource", Value: "/private", Ports: []int{80}, Deny: true}}, target) == nil {
		t.Fatal("unsupported deny was silently ignored")
	}
}
func FuzzControlTargetRoundTrip(f *testing.F) {
	for _, s := range []string{"http://localhost/", "https://[::1]:443/a", "http://example.invalid:80/", "http://user@localhost/"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		target, err := NormalizeTarget(s)
		if err != nil {
			return
		}
		again, err := NormalizeTarget(target.URL())
		if err != nil || target != again {
			t.Fatalf("unstable normalization: %+v %+v %v", target, again, err)
		}
	})
}
