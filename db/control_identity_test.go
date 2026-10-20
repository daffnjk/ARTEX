package db

import (
	"os"
	"testing"
)

func TestControlTaskExplorationIdentity(t *testing.T) {
	if os.Getenv("ARTEX_TEST_PG_DSN") == "" {
		t.Fatal("explicit isolated ARTEX_TEST_PG_DSN is required")
	}
	dsn := researchPlanDSN(t)
	if dsn != os.Getenv("ARTEX_PG_DSN") {
		t.Fatal("ARTEX_PG_DSN and ARTEX_TEST_PG_DSN must be identical")
	}
	d, err := Open(dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	createTask := func() *Task {
		t.Helper()
		task, err := d.CreateTaskWithOptions("control identity fixture", "synthetic task ownership", TaskCreateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := d.DeleteTask(task.ID); err != nil {
				t.Error(err)
			}
		})
		return task
	}
	task := createTask()
	if task.ID == task.ExplorationID {
		// Advance only the exploration sequence with a real disposable row.
		id, err := d.CreateExploration("identity skew fixture", "not a task")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if _, err := d.Exec(`DELETE FROM explorations WHERE id=$1`, id); err != nil {
				t.Error(err)
			}
		})
		task = createTask()
	}
	if task.ID == task.ExplorationID {
		t.Fatal("fixture must have task_id != exploration_id")
	}
	loaded, err := d.GetTask(task.ID)
	if err != nil || loaded == nil || loaded.ExplorationID != task.ExplorationID {
		t.Fatalf("host task mapping: loaded=%+v err=%v", loaded, err)
	}
	store := d.Exploration(loaded.ExplorationID)
	if owner, err := store.TaskID(); err != nil || owner != task.ID {
		t.Fatalf("exploration owner=%d err=%v, want task %d", owner, err, task.ID)
	}
	description, goal, err := store.Root()
	if err != nil || description != task.Description || goal != task.Goal {
		t.Fatalf("mapped exploration root=%q/%q err=%v", description, goal, err)
	}
	nodeID, err := store.AddNode(KindFact, map[string]any{"summary": "identity owned fact"}, 1, "confirmed", "worker", nil)
	if err != nil {
		t.Fatal(err)
	}
	if node, err := store.GetNode(nodeID); err != nil || node == nil || node.ID != nodeID {
		t.Fatalf("mapped node=%+v err=%v", node, err)
	}
	// Replacing exploration_id with task_id must not resolve the owned node.
	if node, err := d.Exploration(task.ID).GetNode(nodeID); err != nil || node != nil {
		t.Fatalf("confused task/exploration IDs exposed node=%+v err=%v", node, err)
	}
	other := createTask()
	if node, err := d.Exploration(other.ExplorationID).GetNode(nodeID); err != nil || node != nil {
		t.Fatalf("foreign task exposed node=%+v err=%v", node, err)
	}
}
