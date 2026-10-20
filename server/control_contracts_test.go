package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestControlAgentCannotApproveContract(t *testing.T) {
	s := &Server{jwtKey: []byte("synthetic-control-auth-key-for-test-only")}
	called := false
	h := s.requireControlAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		if controlAdminFrom(r.Context()) != "admin:ARTEX" {
			t.Fatal("missing authenticated actor")
		}
		w.WriteHeader(204)
	}))
	for _, subject := range []string{"", "worker", "model", "ordinary-user"} {
		for _, op := range []string{"approve", "revoke"} {
			called = false
			r := httptest.NewRequest("POST", "/api/tasks/1/control/"+op, strings.NewReader(`{"approved_by":"admin:ARTEX","role":"admin"}`))
			r.Header.Set("X-Role", "admin")
			if subject != "" {
				token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{Subject: subject, ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour))}).SignedString(s.jwtKey)
				if err != nil {
					t.Fatal(err)
				}
				r.Header.Set("Authorization", "Bearer "+token)
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r.WithContext(context.Background()))
			if called || w.Code != 403 {
				t.Fatalf("subject %q %s: called=%v status=%d", subject, op, called, w.Code)
			}
		}
	}
	token, err := signJWT(s.jwtKey)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("POST", "/api/tasks/1/control/approve", nil)
	r.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if !called || w.Code != 204 {
		t.Fatal("valid admin rejected", w.Code)
	}
}
