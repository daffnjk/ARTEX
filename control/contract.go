package control

import (
	"errors"
	"time"
)

type MethodRef struct {
	ID      string `json:"id"`
	Version string `json:"version"`
}
type ScopeRule struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
	Ports []int  `json:"ports"`
	Deny  bool   `json:"deny,omitempty"`
}
type BudgetLimit struct {
	Dimension    string `json:"dimension"`
	Limit        int64  `json:"limit"`
	CloseReserve int64  `json:"close_reserve"`
	Measurement  string `json:"measurement"`
}
type Contract struct {
	Mode              TaskMode      `json:"mode"`
	Methods           []MethodRef   `json:"methods"`
	Targets           []ScopeRule   `json:"targets"`
	IdentityRefs      []string      `json:"identity_refs"`
	DataScope         []string      `json:"data_scope"`
	Budgets           []BudgetLimit `json:"budgets"`
	ExpiresAt         time.Time     `json:"expires_at"`
	AcceptanceVersion string        `json:"acceptance_version"`
}

func (c Contract) Validate(now time.Time) error {
	if !c.Mode.Valid() || !c.ExpiresAt.After(now) || !bounded(c.AcceptanceVersion, 128) || len(c.Methods) == 0 || len(c.Methods) > 32 || len(c.Targets) == 0 || len(c.Targets) > 128 || len(c.Budgets) == 0 || len(c.Budgets) > 16 || len(c.DataScope) == 0 || len(c.IdentityRefs) > 32 {
		return errors.New("invalid or expired control contract")
	}
	seen := map[string]bool{}
	for _, m := range c.Methods {
		key := m.ID + ":" + m.Version
		if !bounded(m.ID, 120) || !bounded(m.Version, 64) || seen[key] {
			return errors.New("invalid method whitelist")
		}
		seen[key] = true
	}
	for _, r := range c.Targets {
		if err := ValidateScopeRule(r); err != nil {
			return err
		}
	}
	for _, v := range append(append([]string{}, c.IdentityRefs...), c.DataScope...) {
		if !bounded(v, 128) {
			return errors.New("invalid data or identity reference")
		}
	}
	seen = map[string]bool{}
	for _, b := range c.Budgets {
		if !bounded(b.Dimension, 64) || b.Limit < 1 || b.CloseReserve < 0 || b.CloseReserve > b.Limit || (b.Measurement != "exact" && b.Measurement != "estimated") || seen[b.Dimension] {
			return errors.New("invalid budget")
		}
		seen[b.Dimension] = true
	}
	return nil
}
