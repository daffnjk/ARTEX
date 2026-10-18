package server

import (
	"fmt"
	"net/http"

	"github.com/Autumn-27/artex/db"
)

// M2.3（research）：只读研究检查面。
//
// 一个端点聚合任务级的"研究可见性"材料：完成事实（状态/时间/来源分记）、
// 图一致性诊断（限本任务探索）、评估存档（append-only harness_evaluations）、
// 当前/历史冻结验收版本。全部只读——不提供任何修改路径。

// researchInspection 是 GET /api/tasks/{id}/research/inspection 的载荷。
type researchInspection struct {
	Task struct {
		ID              string `json:"id"`
		Status          string `json:"status"`
		CompletedAt     string `json:"completed_at,omitempty"`
		CompletedSource string `json:"completed_source,omitempty"`
	} `json:"task"`
	Integrity   *db.IntegrityReportScoped `json:"integrity,omitempty"`
	Evaluations []evalRecordDTO           `json:"evaluations"`
	Acceptance  []acceptanceRecordDTO     `json:"acceptance"`
}

type evalRecordDTO struct {
	RequestID   string `json:"request_id"`
	GoalID      int64  `json:"goal_id"`
	Validity    string `json:"validity"`
	Conclusion  string `json:"conclusion"`
	Disposition string `json:"disposition"`
	CreatedAt   string `json:"created_at"`
}

type acceptanceRecordDTO struct {
	GoalID    int64  `json:"goal_id"`
	Version   string `json:"version"`
	Claim     string `json:"claim"`
	Current   bool   `json:"current"`
	CreatedAt string `json:"created_at"`
}

// buildResearchInspection 聚合检查面（纯读、可测：不依赖 HTTP/Server 装配）。
func buildResearchInspection(pg *db.DB, id string) (researchInspection, int, error) {
	var out researchInspection
	taskID, err := parseTaskID(id)
	if err != nil || taskID <= 0 {
		return out, 404, fmt.Errorf("invalid task id")
	}
	var status, completedAt, completedSource string
	err = pg.QueryRow(`SELECT status,
		COALESCE(to_char(completed_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'), ''),
		COALESCE(completed_source, '')
		FROM tasks WHERE id=$1 AND deleted_at IS NULL`, taskID).
		Scan(&status, &completedAt, &completedSource)
	if err != nil {
		return out, 404, fmt.Errorf("task not found")
	}
	out.Task.ID = id
	out.Task.Status = status
	out.Task.CompletedAt = completedAt
	out.Task.CompletedSource = completedSource
	if err := pg.TaskIntegrityReportScoped(taskID, &out.Integrity); err != nil {
		return out, 500, err
	}
	records, err := pg.EvaluationRecords(taskID, 50)
	if err != nil {
		return out, 500, err
	}
	out.Evaluations = make([]evalRecordDTO, 0, len(records))
	for _, e := range records {
		out.Evaluations = append(out.Evaluations, evalRecordDTO{
			RequestID: e.RequestID, GoalID: e.GoalID, Validity: e.Validity,
			Conclusion: e.Conclusion, Disposition: e.Disposition, CreatedAt: e.CreatedAt,
		})
	}
	history, err := pg.HarnessAcceptanceTaskHistory(taskID)
	if err != nil {
		return out, 500, err
	}
	out.Acceptance = make([]acceptanceRecordDTO, 0, len(history))
	for _, h := range history {
		out.Acceptance = append(out.Acceptance, acceptanceRecordDTO{
			GoalID: h.GoalID, Version: h.Version, Claim: h.Claim, Current: h.Current,
			CreatedAt: h.CreatedAt,
		})
	}
	return out, 200, nil
}

func (s *Server) taskResearchInspection(w http.ResponseWriter, r *http.Request) {
	out, code, err := buildResearchInspection(s.m.pg, r.PathValue("id"))
	if err != nil {
		writeErr(w, code, err.Error())
		return
	}
	writeJSON(w, 200, out)
}
