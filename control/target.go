package control

import (
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"unicode"
)

func (t Target) URL() string {
	return t.Scheme + "://" + net.JoinHostPort(t.Host, strconv.Itoa(t.Port)) + t.Path
}

// NormalizeTarget is deliberately narrower than a browser URL parser.
func NormalizeTarget(raw string) (Target, error) {
	var t Target
	if raw != strings.TrimSpace(raw) || strings.ContainsAny(raw, "\\%") || strings.IndexFunc(raw, unicode.IsSpace) >= 0 {
		return t, ScopeDenied
	}
	u, err := url.Parse(raw)
	if err != nil {
		return t, ScopeDenied
	}
	if u.Scheme != "http" && u.Scheme != "https" || u.Opaque != "" || u.User != nil || u.Fragment != "" || u.RawQuery != "" || u.ForceQuery || u.RawPath != "" {
		return t, ScopeDenied
	}
	host, err := normalizeHost(u.Hostname())
	if err != nil {
		return t, err
	}
	port := 80
	if u.Scheme == "https" {
		port = 443
	}
	if p := u.Port(); p != "" {
		n, e := strconv.Atoi(p)
		if e != nil || n < 1 || n > 65535 || strconv.Itoa(n) != p {
			return t, ScopeDenied
		}
		port = n
	} else if strings.HasSuffix(u.Host, ":") {
		return t, ScopeDenied
	}
	path := u.Path
	if path == "" {
		path = "/"
	}
	if !strings.HasPrefix(path, "/") || len(path) > 2048 {
		return t, ScopeDenied
	}
	for _, seg := range strings.Split(path, "/") {
		if seg == ".." || seg == "." {
			return t, ScopeDenied
		}
	}
	return Target{Scheme: u.Scheme, Host: host, Port: port, Path: path}, nil
}
func normalizeHost(raw string) (string, error) {
	if raw == "" || len(raw) > 253 || strings.ContainsAny(raw, "%/@\\[]?#") {
		return "", ScopeDenied
	}
	if ip, err := netip.ParseAddr(raw); err == nil {
		if ip.Zone() != "" || ip.Is4In6() {
			return "", ScopeDenied
		}
		return ip.String(), nil
	}
	host := strings.ToLower(strings.TrimSuffix(raw, "."))
	if strings.HasSuffix(host, ".") || strings.HasPrefix(host, "0x") {
		return "", ScopeDenied
	}
	numeric := true
	for _, c := range host {
		if c != '.' && (c < '0' || c > '9') {
			numeric = false
		}
		if c > 127 {
			return "", ScopeDenied
		}
	}
	if numeric {
		return "", ScopeDenied
	}
	for _, label := range strings.Split(host, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return "", ScopeDenied
		}
		for _, c := range label {
			if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
				return "", ScopeDenied
			}
		}
	}
	return host, nil
}
func ValidateScopeRule(r ScopeRule) error {
	if len(r.Ports) == 0 || len(r.Ports) > 32 {
		return ScopeDenied
	}
	for _, p := range r.Ports {
		if p < 1 || p > 65535 {
			return ScopeDenied
		}
	}
	switch r.Kind {
	case "exact_host":
		_, err := normalizeHost(r.Value)
		return err
	case "domain_subtree":
		h, err := normalizeHost(r.Value)
		if err != nil {
			return err
		}
		if _, err = netip.ParseAddr(h); err == nil {
			return ScopeDenied
		}
		return nil
	case "cidr":
		prefix, err := netip.ParsePrefix(r.Value)
		if err != nil || prefix != prefix.Masked() || prefix.Addr().Is4In6() {
			return ScopeDenied
		}
		return nil
	default:
		return ScopeDenied
	}
}
func MatchTarget(rules []ScopeRule, target Target) error {
	t, err := NormalizeTarget(target.URL())
	if err != nil {
		return err
	}
	allowed := false
	for _, r := range rules {
		if err := ValidateScopeRule(r); err != nil {
			return err
		}
		portOK := false
		for _, port := range r.Ports {
			portOK = portOK || port == t.Port
		}
		if !portOK {
			continue
		}
		matches := false
		switch r.Kind {
		case "exact_host":
			h, _ := normalizeHost(r.Value)
			matches = h == t.Host
		case "domain_subtree":
			h, _ := normalizeHost(r.Value)
			matches = t.Host == h || strings.HasSuffix(t.Host, "."+h)
		case "cidr":
			prefix, _ := netip.ParsePrefix(r.Value)
			ip, e := netip.ParseAddr(t.Host)
			matches = e == nil && prefix.Contains(ip)
		}
		if matches && r.Deny {
			return ScopeDenied
		}
		allowed = allowed || matches
	}
	if !allowed {
		return ScopeDenied
	}
	return nil
}

// CheckResolved binds logical authority and every returned connection address.
// DNS itself is an adapter responsibility; empty or mixed-authorization answers fail closed.
func CheckResolved(rules []ScopeRule, target Target, ips []netip.Addr) error {
	if err := MatchTarget(rules, target); err != nil {
		return err
	}
	if len(ips) == 0 || len(ips) > 64 {
		return ScopeDenied
	}
	for _, ip := range ips {
		if !ip.IsValid() || ip.Zone() != "" || ip.Is4In6() {
			return ScopeDenied
		}
		peer := target
		peer.Host = ip.String()
		if err := MatchTarget(rules, peer); err != nil {
			return fmt.Errorf("resolved address not authorized: %w", ScopeDenied)
		}
	}
	return nil
}
