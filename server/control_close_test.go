package server

import (
	"github.com/Autumn-27/artex/control"
	"github.com/Autumn-27/artex/db"
	"strings"
	"testing"
)

func TestControlReportWithoutLLM(t *testing.T) {
	s, task, _, _, _ := workerControlFixture(t, "http://127.0.0.1:8080/report")
	if _, err := s.m.pg.TryCloseTask(t.Context(), control.TaskID(task.ID), db.ControlCloseRequest{Reason: control.StopUnknown, Source: "host:fixture"}); err != nil {
		t.Fatal(err)
	}
	text, err := s.renderControlReport(t.Context(), control.TaskID(task.ID))
	if err != nil || !strings.Contains(text, "inconclusive") || !strings.Contains(text, "cleanup") || !strings.Contains(text, "unknown") {
		t.Fatal("host report incomplete", text, err)
	}
}
