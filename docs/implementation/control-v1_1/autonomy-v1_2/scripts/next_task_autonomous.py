#!/usr/bin/env python3
"""Read-only task selection for ARTEX's delegated development workflow.

This is NOT an execution service, test verifier, review authority, or Git client.
The coding agent must validate actual evidence before recording verified_local.
Independent review status does not block development dependency resolution.
Python 3.10+, standard library only.
"""
from __future__ import annotations

import argparse
import json
from pathlib import Path
import sys
from typing import Any

SATISFIED = frozenset({"verified_local", "done_verified"})
ALLOWED = SATISFIED | {"pending", "in_progress", "needs_review", "blocked"}


def choose_next(plan: dict[str, Any], progress: dict[str, Any]) -> dict[str, Any]:
    """Select a single work item without modifying any file or approving work."""
    tasks = plan.get("tasks")
    states = progress.get("tasks")
    if not isinstance(tasks, list) or not tasks:
        raise ValueError("tasks.json must contain a nonempty tasks list")
    if not isinstance(states, dict):
        raise ValueError("progress.json must contain a tasks object")

    indexed: dict[str, dict[str, Any]] = {}
    for task in tasks:
        if not isinstance(task, dict):
            raise ValueError("invalid task entry")
        tid = task.get("id")
        if not isinstance(tid, str) or not tid or tid in indexed:
            raise ValueError("missing or duplicate task ID")
        deps = task.get("depends_on")
        if not isinstance(deps, list) or any(not isinstance(d, str) for d in deps):
            raise ValueError(f"invalid dependencies: {tid}")
        if len(set(deps)) != len(deps):
            raise ValueError(f"duplicate dependencies: {tid}")
        indexed[tid] = task

    if set(states) != set(indexed):
        raise ValueError("plan/progress task IDs differ; reconcile without resetting history")
    for tid, record in states.items():
        if not isinstance(record, dict) or record.get("status") not in ALLOWED:
            raise ValueError(f"unknown task status: {tid}")
        for dep in indexed[tid]["depends_on"]:
            if dep not in indexed:
                raise ValueError(f"unknown dependency {dep} of {tid}")

    visiting: set[str] = set()
    visited: set[str] = set()

    def walk(tid: str) -> None:
        if tid in visiting:
            raise ValueError(f"dependency cycle at {tid}")
        if tid in visited:
            return
        visiting.add(tid)
        for dep in indexed[tid]["depends_on"]:
            walk(dep)
        visiting.remove(tid)
        visited.add(tid)

    for tid in indexed:
        walk(tid)

    def unmet(tid: str) -> list[str]:
        return [d for d in indexed[tid]["depends_on"] if states[d]["status"] not in SATISFIED]

    # A contradictory completed record is not silently used to unlock descendants.
    for tid in indexed:
        if states[tid]["status"] in SATISFIED and unmet(tid):
            raise ValueError(f"completed task has unsatisfied dependencies: {tid}")

    active = [tid for tid in indexed if states[tid]["status"] == "in_progress"]
    if len(active) > 1:
        raise ValueError("multiple active tickets; reconcile single-writer state first")
    if active:
        tid = active[0]
        if unmet(tid):
            raise ValueError(f"active task has unsatisfied dependencies: {tid}")
        return {"action": "resume", "task_id": tid, "ticket": f"tasks/{tid}.md"}

    for tid in indexed:
        if states[tid]["status"] == "needs_review" and not unmet(tid):
            return {
                "action": "verify_local_evidence",
                "task_id": tid,
                "ticket": f"tasks/{tid}.md",
                "instruction": "Validate current implementation, required tests, regressions and receipts; "
                               "only then record verified_local. Preserve actual review status; "
                               "no human approval is required for this development transition.",
            }

    for tid in indexed:
        if states[tid]["status"] == "pending" and not unmet(tid):
            return {"action": "start", "task_id": tid, "ticket": f"tasks/{tid}.md"}

    if all(record["status"] in SATISFIED for record in states.values()):
        return {
            "action": "final_integration",
            "instruction": "Run final-tree aggregate acceptance, regression, migration, security "
                           "and delivery checks. Only after success may the authorized coding "
                           "agent push the verified delivery branch. This script has not "
                           "tested, committed, pushed, merged or deployed anything.",
        }

    return {
        "action": "blocked",
        "reason": "No eligible development task; do not turn actual failures into PASS.",
        "tasks": [
            {"task_id": tid, "status": states[tid]["status"],
             "unmet_dependencies": unmet(tid), "blockers": states[tid].get("blockers", [])}
            for tid in indexed if states[tid]["status"] not in SATISFIED
        ],
    }


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--plan-root", type=Path,
                        default=Path(__file__).resolve().parents[2],
                        help="Existing control-v1_1 directory (not repository root)")
    args = parser.parse_args(argv)
    try:
        root = args.plan_root.resolve(strict=True)
        plan = json.loads((root / "tasks.json").read_text(encoding="utf-8"))
        progress = json.loads((root / "progress.json").read_text(encoding="utf-8"))
        if not isinstance(plan, dict) or not isinstance(progress, dict):
            raise ValueError("plan and progress must be JSON objects")
        result = choose_next(plan, progress)
        result["notice"] = "Read-only scheduling advice; no test/review/Git execution or approval."
        print(json.dumps(result, ensure_ascii=False, indent=2))
        return 3 if result["action"] == "blocked" else 0
    except (OSError, ValueError, KeyError, TypeError) as exc:
        print(json.dumps({"action": "error", "reason": str(exc)}, ensure_ascii=False))
        return 2


if __name__ == "__main__":
    sys.exit(main())
