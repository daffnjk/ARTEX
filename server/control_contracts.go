package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/Autumn-27/artex/control"
	"github.com/golang-jwt/jwt/v5"
)

type controlAdminKey struct{}

func controlAdminFrom(ctx context.Context) string {
	v, _ := ctx.Value(controlAdminKey{}).(string)
	return v
}

// ARTEX currently has one authenticated administrator, not a client-supplied role.
func (s *Server) requireControlAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" && r.Method != "HEAD" && !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
			writeErr(w, 403, "explicit administrator bearer authorization required")
			return
		}
		token, err := jwt.ParseWithClaims(extractToken(r), &jwt.RegisteredClaims{}, func(t *jwt.Token) (any, error) { return s.jwtKey, nil }, jwt.WithValidMethods([]string{"HS256"}), jwt.WithExpirationRequired())
		if err != nil || !token.Valid {
			writeErr(w, 403, "authenticated control administrator required")
			return
		}
		claims, ok := token.Claims.(*jwt.RegisteredClaims)
		if !ok || claims.Subject != "ARTEX" {
			writeErr(w, 403, "authenticated control administrator required")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), controlAdminKey{}, "admin:ARTEX")))
	})
}

// Staged handler: mounted only when the controlled executor is wired (ENG-07).
func (s *Server) controlContractChange(w http.ResponseWriter, r *http.Request) {
	actor := controlAdminFrom(r.Context())
	if actor == "" {
		writeErr(w, 403, "control administrator required")
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeErr(w, 400, "invalid task ID")
		return
	}
	if operation := r.PathValue("operation"); operation != "approve" && operation != "revoke" {
		s.controlAdminOperation(w, r)
		return
	}
	var input struct {
		ExpectedEpoch int64             `json:"expected_epoch"`
		Contract      *control.Contract `json:"contract,omitempty"`
		Reason        string            `json:"reason,omitempty"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 32768)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err = dec.Decode(&input); err != nil {
		writeErr(w, 400, "invalid contract request")
		return
	}
	if dec.Decode(new(any)) != io.EOF {
		writeErr(w, 400, "single JSON object required")
		return
	}
	pg := s.pg(w)
	if pg == nil {
		return
	}
	if r.PathValue("operation") == "revoke" {
		if input.Contract != nil {
			writeErr(w, 400, "revocation does not accept a contract")
			return
		}
		var epoch int64
		err := s.controlGate().RevokeTask(r.Context(), control.TaskID(id), func(ctx context.Context) error {
			var revokeErr error
			epoch, revokeErr = pg.RevokeControlContract(ctx, id, input.ExpectedEpoch, actor, input.Reason)
			return revokeErr
		})
		if err != nil {
			writeErr(w, 409, err.Error())
			return
		}
		writeJSON(w, 200, map[string]any{"control_epoch": epoch})
		return
	}
	if r.PathValue("operation") != "approve" || input.Contract == nil {
		writeErr(w, 400, "contract required")
		return
	}
	out, err := pg.ApproveControlContract(r.Context(), id, input.ExpectedEpoch, *input.Contract, actor)
	if err != nil {
		writeErr(w, 409, err.Error())
		return
	}
	s.controlGate().ResumeTask(control.TaskID(id))
	s.invalidateTaskAgents()
	writeJSON(w, 200, out)
}
