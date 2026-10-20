package execution

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"time"

	"github.com/Autumn-27/artex/control"
)

type PolicySource interface {
	GrantChecker
	ExecutionPolicy(context.Context, control.TaskID, control.ExecutionID) (Record, control.Contract, error)
}
type Resolver interface {
	LookupNetIP(context.Context, string, string) ([]netip.Addr, error)
}
type HTTPHop struct {
	Target     control.Target
	PeerIP     netip.Addr
	StatusCode int
	Headers    map[string][]string
	Body       []byte
	SHA256     string
	Complete   bool
}
type HTTPResult struct {
	ExecutionID             control.ExecutionID
	ContextVersion          string
	Hops                    []HTTPHop
	Requests, ResponseBytes int64
	Complete                bool
	Outcome                 control.Outcome
}
type HTTPAdapter struct {
	source     PolicySource
	supervisor *Supervisor
	resolver   Resolver
	roots      *x509.CertPool
}

func NewHTTPAdapter(source PolicySource, supervisor *Supervisor, resolver Resolver, roots *x509.CertPool) *HTTPAdapter {
	if roots != nil {
		roots = roots.Clone()
	}
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	return &HTTPAdapter{source, supervisor, resolver, roots}
}
func (a *HTTPAdapter) Run(ctx context.Context, task control.TaskID, id control.ExecutionID) (result HTTPResult, err error) {
	result = HTTPResult{ExecutionID: id, Outcome: control.Outcome{Run: control.RunFailed, Effect: control.EffectNotApplied, Cleanup: control.CleanupConfirmed}}
	if a.source == nil || a.supervisor == nil {
		return result, control.ToolUnavailable
	}
	record, contract, err := a.source.ExecutionPolicy(ctx, task, id)
	if err != nil {
		return result, err
	}
	if record.TaskID != task || record.ID != id {
		return result, control.LineageMismatch
	}
	if record.Proposal.MethodID != "http.get" || record.Proposal.MethodVersion != "1" || record.Proposal.IdentityRef != nil && *record.Proposal.IdentityRef != "anonymous" {
		return result, control.MethodUnsupported
	}
	if record.GrantExpires.IsZero() {
		return result, control.ContractExpired
	}
	ctx, deadlineCancel := context.WithDeadline(ctx, record.GrantExpires)
	defer deadlineCancel()
	ctx, timeoutCancel := context.WithTimeout(ctx, HTTPTimeout)
	defer timeoutCancel()
	ctx, stop, err := a.supervisor.start(ctx, a.source, task, id)
	if err != nil {
		return result, err
	}
	defer stop()
	result.ContextVersion = record.Proposal.ContextVersion
	target := record.Proposal.Target
	for result.Requests < HTTPMaxRequests {
		target, err = control.NormalizeTarget(target.URL())
		if err != nil {
			return result, err
		}
		var ips []netip.Addr
		if ip, e := netip.ParseAddr(target.Host); e == nil {
			if !ip.IsLoopback() {
				return result, control.ScopeDenied
			}
			ips = []netip.Addr{ip}
		} else {
			if target.Host != "localhost" {
				return result, control.ScopeDenied
			}
			ips, err = a.resolver.LookupNetIP(ctx, "ip", target.Host)
			if err != nil {
				return result, err
			}
		}
		if err = control.CheckResolved(contract.Targets, target, ips); err != nil {
			return result, err
		}
		for _, ip := range ips {
			if !ip.IsLoopback() {
				return result, control.ScopeDenied
			}
		}
		peer := ips[0]
		authority := net.JoinHostPort(target.Host, strconv.Itoa(target.Port))
		pinned := net.JoinHostPort(peer.String(), strconv.Itoa(target.Port))
		transport := &http.Transport{Proxy: nil, DisableKeepAlives: true, DisableCompression: true, TLSHandshakeTimeout: time.Second, ResponseHeaderTimeout: HTTPTimeout, MaxResponseHeaderBytes: 16384, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: a.roots, ServerName: target.Host}, DialContext: func(dialCtx context.Context, network, address string) (net.Conn, error) {
			if address != authority {
				return nil, control.ScopeDenied
			}
			if err := a.supervisor.before(dialCtx, a.source, task, id); err != nil {
				return nil, err
			}
			return (&net.Dialer{Timeout: time.Second}).DialContext(dialCtx, "tcp", pinned)
		}}
		client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		request, e := http.NewRequestWithContext(ctx, http.MethodGet, target.URL(), nil)
		if e != nil {
			transport.CloseIdleConnections()
			return result, e
		}
		request.Header.Set("Accept", "application/json")
		request.Header.Set("X-ARTEX-Execution-ID", string(id))
		request.Header.Set("User-Agent", "ARTEX-local-fixture/1")
		if err = a.supervisor.before(ctx, a.source, task, id); err != nil {
			transport.CloseIdleConnections()
			return result, err
		}
		result.Requests++
		result.Outcome.Effect = control.EffectUnknown
		response, e := client.Do(request)
		if e != nil {
			transport.CloseIdleConnections()
			return result, e
		}
		payload, readErr := io.ReadAll(io.LimitReader(response.Body, HTTPMaxResponseBytes+1))
		closeErr := response.Body.Close()
		transport.CloseIdleConnections()
		complete := readErr == nil && closeErr == nil && int64(len(payload)) <= HTTPMaxResponseBytes
		if int64(len(payload)) > HTTPMaxResponseBytes {
			payload = payload[:HTTPMaxResponseBytes]
		}
		headers := map[string][]string{}
		for _, name := range []string{"Content-Type", "Content-Length", "Location"} {
			if values := response.Header.Values(name); len(values) > 0 {
				headers[name] = append([]string{}, values...)
			}
		}
		sum := sha256.Sum256(payload)
		result.Hops = append(result.Hops, HTTPHop{target, peer, response.StatusCode, headers, payload, hex.EncodeToString(sum[:]), complete})
		result.ResponseBytes += int64(len(payload))
		if !complete {
			return result, control.ObservationIncomplete
		}
		if response.StatusCode >= 300 && response.StatusCode < 400 {
			location, e := response.Location()
			if e != nil {
				return result, control.ObservationIncomplete
			}
			target, e = control.NormalizeTarget(location.String())
			if e != nil {
				return result, e
			}
			continue
		}
		result.Complete = true
		result.Outcome = control.Outcome{Run: control.RunSucceeded, Effect: control.EffectUnknown, Cleanup: control.CleanupConfirmed}
		return result, nil
	}
	return result, control.UnboundedEffect
}
