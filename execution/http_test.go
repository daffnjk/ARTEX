package execution

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Autumn-27/artex/control"
)

type localGrantFixture struct {
	record   Record
	contract control.Contract
	revoked  atomic.Bool
}

func (f *localGrantFixture) CheckExecutionGrant(_ context.Context, task control.TaskID, id control.ExecutionID) error {
	if task != f.record.TaskID || id != f.record.ID {
		return control.LineageMismatch
	}
	if f.revoked.Load() {
		return control.ContractRevoked
	}
	return nil
}
func (f *localGrantFixture) ExecutionPolicy(ctx context.Context, task control.TaskID, id control.ExecutionID) (Record, control.Contract, error) {
	err := f.CheckExecutionGrant(ctx, task, id)
	return f.record, f.contract, err
}

type fixtureResolver func(context.Context, string, string) ([]netip.Addr, error)

func (f fixtureResolver) LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error) {
	return f(ctx, network, host)
}
func localHTTPFixture(t *testing.T, url string) (*localGrantFixture, *HTTPAdapter) {
	t.Helper()
	target, err := control.NormalizeTarget(url)
	if err != nil {
		t.Fatal(err)
	}
	source := &localGrantFixture{record: Record{ID: "fixture-execution", TaskID: 1, ExplorationID: 2, Proposal: control.ActionProposal{MethodID: "http.get", MethodVersion: "1", Target: target, ContextVersion: "initial"}, GrantExpires: time.Now().Add(time.Minute)}, contract: control.Contract{Targets: []control.ScopeRule{{Kind: "exact_host", Value: target.Host, Ports: []int{target.Port}}, {Kind: "cidr", Value: "127.0.0.0/8", Ports: []int{target.Port}}}}}
	return source, NewHTTPAdapter(source, NewSupervisor(), nil, nil)
}
func TestControlRedirectBoundary(t *testing.T) {
	var deniedHits atomic.Int64
	denied := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { deniedHits.Add(1); _, _ = w.Write([]byte("denied")) }))
	defer denied.Close()
	allowed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, denied.URL, 302) }))
	defer allowed.Close()
	f, a := localHTTPFixture(t, allowed.URL)
	result, err := a.Run(t.Context(), f.record.TaskID, f.record.ID)
	if err == nil || deniedHits.Load() != 0 || result.Requests != 1 {
		t.Fatal("redirect escaped", deniedHits.Load(), result, err)
	}
}
func TestControlPinnedDial(t *testing.T) {
	var hits atomic.Int64
	var host atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host.Store(r.Host)
		hits.Add(1)
		_, _ = w.Write([]byte("pinned fixture"))
	}))
	defer srv.Close()
	f, a := localHTTPFixture(t, strings.Replace(srv.URL, "127.0.0.1", "localhost", 1))
	var lookups atomic.Int64
	a.resolver = fixtureResolver(func(context.Context, string, string) ([]netip.Addr, error) {
		if lookups.Add(1) == 1 {
			return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
		}
		return []netip.Addr{netip.MustParseAddr("192.0.2.10")}, nil
	})
	result, err := a.Run(t.Context(), f.record.TaskID, f.record.ID)
	if err != nil || !result.Complete || hits.Load() != 1 || lookups.Load() != 1 || host.Load() != fmt.Sprintf("localhost:%d", f.record.Proposal.Target.Port) {
		t.Fatal("dial was not pinned or Host changed", result, err, hits.Load(), lookups.Load(), host.Load())
	}
}
func TestControlResponseLimit(t *testing.T) {
	for _, slow := range []bool{false, true} {
		t.Run(fmt.Sprint(slow), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if slow {
					_, _ = w.Write([]byte("partial"))
					w.(http.Flusher).Flush()
					<-r.Context().Done()
					return
				}
				_, _ = w.Write([]byte(strings.Repeat("x", int(HTTPMaxResponseBytes)+100)))
			}))
			defer srv.Close()
			f, a := localHTTPFixture(t, srv.URL)
			started := time.Now()
			result, err := a.Run(t.Context(), f.record.TaskID, f.record.ID)
			if err == nil || result.Complete || time.Since(started) > HTTPTimeout+time.Second || result.ResponseBytes > HTTPMaxResponseBytes*HTTPMaxRequests {
				t.Fatal("unbounded or incorrectly complete response", result, err)
			}
		})
	}
}
func TestControlGrantRevokedBeforeDial(t *testing.T) {
	var hits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1) }))
	defer srv.Close()
	f, a := localHTTPFixture(t, strings.Replace(srv.URL, "127.0.0.1", "localhost", 1))
	a.resolver = fixtureResolver(func(context.Context, string, string) ([]netip.Addr, error) {
		f.revoked.Store(true)
		return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
	})
	if _, err := a.Run(t.Context(), f.record.TaskID, f.record.ID); err == nil || hits.Load() != 0 {
		t.Fatal("revoked connection reached target", hits.Load(), err)
	}
}
func TestControlProxyEnvCannotBypass(t *testing.T) {
	var proxyHits atomic.Int64
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { proxyHits.Add(1) }))
	defer proxy.Close()
	for _, key := range []string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "http_proxy", "https_proxy"} {
		t.Setenv(key, proxy.URL)
	}
	t.Setenv("NO_PROXY", "")
	var targetHits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetHits.Add(1)
		_, _ = w.Write([]byte("direct synthetic"))
	}))
	defer srv.Close()
	f, a := localHTTPFixture(t, srv.URL)
	if _, err := a.Run(t.Context(), f.record.TaskID, f.record.ID); err != nil || proxyHits.Load() != 0 || targetHits.Load() != 1 {
		t.Fatal("proxy changed connection path", proxyHits.Load(), targetHits.Load(), err)
	}
}

func TestControlTLSHostIdentity(t *testing.T) {
	var hits atomic.Int64
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1); _, _ = w.Write([]byte("tls fixture")) }))
	defer srv.Close()
	roots := x509.NewCertPool()
	roots.AddCert(srv.Certificate())
	f, a := localHTTPFixture(t, srv.URL)
	a.roots = roots
	if _, err := a.Run(t.Context(), f.record.TaskID, f.record.ID); err != nil || hits.Load() != 1 {
		t.Fatal("trusted TLS fixture failed", hits.Load(), err)
	}
	f, a = localHTTPFixture(t, strings.Replace(srv.URL, "127.0.0.1", "localhost", 1))
	a.roots = roots
	a.resolver = fixtureResolver(func(context.Context, string, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
	})
	_, err := a.Run(t.Context(), f.record.TaskID, f.record.ID)
	var hostname x509.HostnameError
	if !errors.As(err, &hostname) || hits.Load() != 1 {
		t.Fatal("TLS identity was not checked against logical host", hits.Load(), err)
	}
}
func TestControlSupervisorCancelsActiveConnection(t *testing.T) {
	started, closed := make(chan struct{}), make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("started"))
		w.(http.Flusher).Flush()
		close(started)
		<-r.Context().Done()
		close(closed)
	}))
	defer srv.Close()
	f, a := localHTTPFixture(t, srv.URL)
	done := make(chan error, 1)
	go func() { _, err := a.Run(t.Context(), f.record.TaskID, f.record.ID); done <- err }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("fixture not reached")
	}
	if err := a.supervisor.RevokeTask(t.Context(), f.record.TaskID, func(context.Context) error { f.revoked.Store(true); return nil }); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancellation reported complete response")
		}
	case <-time.After(time.Second):
		t.Fatal("local cancellation exceeded one second")
	}
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("server connection still open after cancellation")
	}
}
