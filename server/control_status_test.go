package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/Autumn-27/artex/control"
	"github.com/Autumn-27/artex/db"
	"github.com/golang-jwt/jwt/v5"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func controlRequest(t *testing.T, s *Server, task int64, method, operation, subject string, body any) *httptest.ResponseRecorder {
	t.Helper()
	s.jwtKey = []byte("synthetic-control-ui-signing-key")
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(method, fmt.Sprintf("/api/tasks/%d/control/%s", task, operation), bytes.NewReader(raw))
	request.SetPathValue("id", strconv.FormatInt(task, 10))
	request.SetPathValue("operation", operation)
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{Subject: subject, ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour))}).SignedString(s.jwtKey)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler := s.controlStatus
	if method == http.MethodPost {
		handler = s.controlContractChange
	}
	s.requireControlAdmin(http.HandlerFunc(handler)).ServeHTTP(response, request)
	return response
}
func TestControlStatusOwnership(t *testing.T) {
	s, a, _, _, _ := workerControlFixture(t, "http://127.0.0.1:8080/owned")
	_, b, _, _, _ := workerControlFixture(t, "http://127.0.0.1:8080/foreign", func(c *control.Contract) { c.IdentityRefs = append(c.IdentityRefs, "SECRET_REFERENCE_MUST_NOT_LEAK") })
	denied := controlRequest(t, s, a.ID, http.MethodGet, "status", "worker", nil)
	if denied.Code != 403 {
		t.Fatal("worker read controller state", denied.Code)
	}
	response := controlRequest(t, s, a.ID, http.MethodGet, "status", "ARTEX", nil)
	if response.Code != 200 {
		t.Fatal(response.Code, response.Body.String())
	}
	var payload struct {
		TaskID int64 `json:"task_id"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil || payload.TaskID != a.ID || payload.TaskID == b.ID || strings.Contains(response.Body.String(), "SECRET_REFERENCE_MUST_NOT_LEAK") {
		t.Fatal("cross-task controller disclosure", payload, err)
	}
	response = controlRequest(t, s, b.ID, http.MethodGet, "status", "ARTEX", nil)
	if response.Code != 200 || strings.Contains(response.Body.String(), "SECRET_REFERENCE_MUST_NOT_LEAK") {
		t.Fatal("credential reference exposed", response.Code)
	}
}
func TestControlAdminActionRevision(t *testing.T) {
	s, task, _, _, p := workerControlFixture(t, "http://127.0.0.1:8080/cancel")
	r, err := s.m.pg.AdmitAndReserve(t.Context(), control.TaskID(task.ID), p)
	if err != nil {
		t.Fatal(err)
	}
	response := controlRequest(t, s, task.ID, http.MethodPost, "cancel", "ARTEX", map[string]any{"expected_epoch": 0, "execution_id": r.ID})
	if response.Code != 409 {
		t.Fatal("stale cancel not conflicted", response.Code, response.Body.String())
	}
	still, err := s.m.pg.GetControlExecution(t.Context(), control.TaskID(task.ID), r.ID)
	if err != nil || still.Outcome.Run != control.RunQueued {
		t.Fatal("stale cancel mutated execution", still, err)
	}
	response = controlRequest(t, s, task.ID, http.MethodPost, "cancel", "ARTEX", map[string]any{"expected_epoch": 1, "execution_id": r.ID})
	if response.Code != 200 {
		t.Fatal(response.Code, response.Body.String())
	}
	active, err := s.m.pg.ActiveControlContract(t.Context(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	response = controlRequest(t, s, task.ID, http.MethodPost, "approve", "ARTEX", map[string]any{"expected_epoch": 0, "contract": active.Body})
	if response.Code != 409 {
		t.Fatal("stale approval not conflicted", response.Code)
	}
}
func TestControlStatusPartialVisible(t *testing.T) {
	s, task, _, _, _ := workerControlFixture(t, "http://127.0.0.1:8080/report-evidence")
	testControlReportCapturedLinks(t, s, task)
	if _, err := s.m.pg.TryCloseTask(t.Context(), control.TaskID(task.ID), db.ControlCloseRequest{Reason: control.StopUnknown, Source: "host:fixture"}); err != nil {
		t.Fatal(err)
	}
	response := controlRequest(t, s, task.ID, http.MethodGet, "status", "ARTEX", nil)
	var object map[string]json.RawMessage
	if err := json.Unmarshal(response.Body.Bytes(), &object); err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err := json.Unmarshal(object["result"], &result); err != nil {
		t.Fatal(err)
	}
	if response.Code != 200 || result["result"] != "partial" || result["reason"] != "inconclusive" || result["cleanup"] == nil || result["legacy_status"] != "failed" {
		t.Fatal("partial result hidden behind compatibility status", response.Code, result)
	}
}
