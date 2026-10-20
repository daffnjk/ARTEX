"""Scheduler unit tests only: no ARTEX, database, network, or Git writes."""
from __future__ import annotations
import contextlib
import importlib.util
import io
import json
from pathlib import Path
import tempfile
import unittest

SCRIPT = Path(__file__).resolve().parents[1] / "scripts" / "next_task_autonomous.py"
spec = importlib.util.spec_from_file_location("autonomous_scheduler", SCRIPT)
assert spec is not None and spec.loader is not None
scheduler = importlib.util.module_from_spec(spec)
spec.loader.exec_module(scheduler)


def data(a="pending", b="pending"):
    return ({"tasks": [{"id": "ENG-00", "depends_on": []},
                       {"id": "ENG-01", "depends_on": ["ENG-00"]}]},
            {"tasks": {"ENG-00": {"status": a}, "ENG-01": {"status": b}}})


class SchedulerTests(unittest.TestCase):
    def test_starts_first_pending(self):
        self.assertEqual(scheduler.choose_next(*data())["task_id"], "ENG-00")

    def test_verified_local_unlocks(self):
        self.assertEqual(scheduler.choose_next(*data("verified_local"))["task_id"], "ENG-01")

    def test_legacy_verified_unlocks(self):
        self.assertEqual(scheduler.choose_next(*data("done_verified"))["task_id"], "ENG-01")

    def test_missing_review_does_not_block(self):
        p, s = data("verified_local")
        s["tasks"]["ENG-00"]["review"] = "needs_independent_review"
        self.assertEqual(scheduler.choose_next(p, s)["action"], "start")

    def test_needs_review_requires_evidence_not_approval(self):
        p, s = data("needs_review")
        before = json.dumps(s, sort_keys=True)
        r = scheduler.choose_next(p, s)
        self.assertEqual(r["action"], "verify_local_evidence")
        self.assertEqual(r["task_id"], "ENG-00")
        self.assertEqual(json.dumps(s, sort_keys=True), before)

    def test_active_resumes(self):
        self.assertEqual(scheduler.choose_next(*data("verified_local", "in_progress"))["action"], "resume")

    def test_true_blocker_blocks_dependent(self):
        self.assertEqual(scheduler.choose_next(*data("blocked"))["action"], "blocked")

    def test_independent_task_can_continue(self):
        p, s = data("blocked")
        p["tasks"][1]["depends_on"] = []
        self.assertEqual(scheduler.choose_next(p, s)["task_id"], "ENG-01")

    def test_all_local_is_not_a_push(self):
        self.assertEqual(scheduler.choose_next(*data("verified_local", "verified_local"))["action"], "final_integration")

    def test_unknown_status_rejected(self):
        with self.assertRaises(ValueError): scheduler.choose_next(*data("invented_pass"))

    def test_multiple_active_rejected(self):
        with self.assertRaises(ValueError): scheduler.choose_next(*data("in_progress", "in_progress"))

    def test_active_cannot_bypass_dependencies(self):
        with self.assertRaises(ValueError): scheduler.choose_next(*data("pending", "in_progress"))

    def test_false_completed_descendant_rejected(self):
        with self.assertRaises(ValueError): scheduler.choose_next(*data("blocked", "verified_local"))

    def test_cycle_rejected(self):
        p, s = data()
        p["tasks"][0]["depends_on"] = ["ENG-01"]
        with self.assertRaises(ValueError): scheduler.choose_next(p, s)

    def test_unknown_dependency_rejected(self):
        p, s = data()
        p["tasks"][1]["depends_on"] = ["ENG-99"]
        with self.assertRaises(ValueError): scheduler.choose_next(p, s)

    def test_duplicate_task_rejected(self):
        p, s = data()
        p["tasks"].append(p["tasks"][0])
        with self.assertRaises(ValueError): scheduler.choose_next(p, s)

    def test_missing_progress_rejected(self):
        p, s = data()
        del s["tasks"]["ENG-01"]
        with self.assertRaises(ValueError): scheduler.choose_next(p, s)

    def test_cli_reads_without_writing(self):
        p, s = data("verified_local")
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            for name, obj in [("tasks.json", p), ("progress.json", s)]:
                (root / name).write_text(json.dumps(obj), encoding="utf-8")
            before = {f.name: f.read_bytes() for f in root.iterdir()}
            out = io.StringIO()
            with contextlib.redirect_stdout(out):
                code = scheduler.main(["--plan-root", str(root)])
            self.assertEqual(code, 0)
            self.assertEqual(json.loads(out.getvalue())["task_id"], "ENG-01")
            self.assertEqual(before, {f.name: f.read_bytes() for f in root.iterdir()})

    def test_cli_blocked_exit(self):
        p, s = data("blocked")
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            (root / "tasks.json").write_text(json.dumps(p))
            (root / "progress.json").write_text(json.dumps(s))
            with contextlib.redirect_stdout(io.StringIO()):
                self.assertEqual(scheduler.main(["--plan-root", tmp]), 3)

    def test_cli_missing_file_exit(self):
        with tempfile.TemporaryDirectory() as tmp:
            with contextlib.redirect_stdout(io.StringIO()):
                self.assertEqual(scheduler.main(["--plan-root", tmp]), 2)


if __name__ == "__main__":
    unittest.main()
