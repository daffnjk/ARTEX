#!/usr/bin/env python3
"""Bounded localhost display fixture, not a security/acceptance backend.

No database, real model, target or reusable credential. Owns a temporary Next dev
child and stops it at shutdown/timeout. Use real Go tests for authorization.
"""
import argparse
import base64
import hashlib
import json
import os
from pathlib import Path
import signal
import socket
import subprocess
import threading
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import urlsplit
from uuid import uuid4


def free_port():
    with socket.socket() as probe:
        probe.bind(("127.0.0.1", 0))
        return probe.getsockname()[1]


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--output", required=True)
    parser.add_argument("--seconds", type=int, default=600)
    args = parser.parse_args()
    if not 1 <= args.seconds <= 1800:
        raise SystemExit("fixture timeout must be 1..1800 seconds")
    output = Path(args.output).resolve()
    output.mkdir(parents=True, exist_ok=False)
    body = b"partial synthetic UI fixture body"
    digest = hashlib.sha256(body).hexdigest()
    task = {"id": "1600", "name": "ENG-16 合成部分结果", "description": "仅合成 localhost 界面夹具", "goal": "保留部分结果和未确认移交", "status": "timeout", "created_at": "2026-10-20T00:00:00Z", "active": True, "paused": False, "coverage_enabled": False, "llm_profile_ids": [], "company_ids": [], "source_task_ids": []}
    execution = "00000000-0000-4000-8000-000000001600"
    observation = f"control:{execution}:fixture-attempt:0"
    state = {"task_id": 1600, "mode": "direct", "contract": {"version": 1, "epoch": 2, "revoked": True, "expires_at": "2026-10-21T00:00:00Z", "anonymous_allowed": True, "methods": [{"id": "http.get", "version": "1"}], "targets": [{"kind": "exact_host", "value": "localhost", "ports": [18080]}]}, "result": {"phase": "closed", "run": "closed", "result": "partial", "reason": "budget", "cleanup": "unconfirmed", "legacy_status": "timeout"}, "budgets": [{"dimension": "requests", "measurement": "exact", "limit": 10, "used": 8, "reserved": 1, "close_reserve": 1, "remaining": 0, "overrun": False}], "executions": [{"id": execution, "run": "lost", "effect": "unknown", "cleanup": "unconfirmed", "cancel_requested": True, "context_version": "fixture-v1", "grant_expires": "2026-10-20T00:00:05Z"}], "evidence": [{"id": observation, "sha256": digest, "validity": "invalid", "complete": False, "bytes": len(body), "context_version": "fixture-v1", "execution_id": execution}], "next": {"execution_after": "", "evidence_after": ""}, "review": {"advisory_records": 1, "trust_domain": "same_host_advisory", "independent": False}}
    encode = lambda value: base64.urlsafe_b64encode(json.dumps(value).encode()).decode().rstrip("=")
    token = encode({"alg": "HS256", "typ": "JWT"}) + "." + encode({"sub": "ARTEX", "exp": int(time.time()) + 1800}) + ".synthetic-display-only"
    lock = threading.Lock()

    class Handler(BaseHTTPRequestHandler):
        def log_message(self, *_):
            pass

        def send_value(self, status, value):
            raw = json.dumps(value, ensure_ascii=False).encode()
            self.send_response(status)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(raw)))
            self.end_headers()
            self.wfile.write(raw)
            with lock, (output / "requests.jsonl").open("a") as log:
                log.write(json.dumps({"method": self.command, "path": urlsplit(self.path).path, "status": status}) + "\n")

        def do_POST(self):
            self.rfile.read(min(int(self.headers.get("Content-Length", "0")), 32768))
            if urlsplit(self.path).path == "/api/auth/login":
                self.send_value(200, {"token": token})
            else:
                self.send_value(405, {"error": "display fixture refuses writes"})

        def do_GET(self):
            path = urlsplit(self.path).path
            if path == "/api/ui-fixture-bootstrap":
                # Invalid outside this display-only backend; no real login or terms acceptance.
                html = ("<!doctype html><title>Synthetic UI fixture</title><p>Display fixture only</p><script>localStorage.setItem('artex_token'," + json.dumps(token) + ");document.cookie='artex_token='+encodeURIComponent(" + json.dumps(token) + ")+ '; path=/; SameSite=Lax';location.replace('/function/tasks/detail?id=1600');</script>").encode()
                self.send_response(200)
                self.send_header("Content-Type", "text/html; charset=utf-8")
                self.send_header("Cache-Control", "no-store")
                self.send_header("Content-Length", str(len(html)))
                self.end_headers()
                self.wfile.write(html)
                return
            if path == "/api/health": value = {"fixture": "control-ui-display-only"}
            elif path == "/api/auth/status": value = {"initialized": True}
            elif path == "/api/tasks/1600/control/status": value = state
            elif path.startswith("/api/tasks/1600/control/executions/"): value = {"execution_id": execution, "outcome": {"run": "lost", "effect": "unknown", "cleanup": "unconfirmed"}}
            elif path == "/api/tasks/1600": value = task
            elif path == "/api/tasks": value = [task]
            elif path == "/api/stats": value = {"engine_mode": "idle", "active_task": task, "assets": 0, "findings": 0, "tasks": 1}
            elif path.endswith("/scope"): value = {"scope": []}
            elif path.endswith("/goals"): value = {"goals": []}
            elif path.endswith("/constraints"): value = {"constraints": []}
            elif path.endswith("/intercept-rules"): value = {"rules": []}
            elif path.endswith("/coverage"): value = {"enabled": False, "scope_rows": 0, "denominator": 0, "tested": 0, "pct": None, "by_type": []}
            elif path == "/api/settings": value = {"llm_record": False}
            elif path in ["/api/llm/profiles", "/api/intercept/pending", "/api/tasks/categories", "/api/agents"] or path.endswith("/intercept"): value = []
            else:
                self.send_value(404, {"error": "not part of synthetic display fixture"})
                return
            self.send_value(200, value)

    backend = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
    port = free_port()
    hostname = f"artex-eng16-{uuid4().hex[:8]}.localhost"
    env = os.environ.copy()
    env.update(AUTOPENTEST_API=f"http://127.0.0.1:{backend.server_port}", NEXT_TELEMETRY_DISABLED="1", NEXT_PUBLIC_MOCK="0")
    log = (output / "next.log").open("wb")
    child = subprocess.Popen(["node_modules/.bin/next", "dev", "--hostname", "127.0.0.1", "--port", str(port)], cwd="web", env=env, stdout=log, stderr=subprocess.STDOUT, start_new_session=True)
    metadata = {"kind": "synthetic_ui_display_only", "url": f"http://{hostname}:{port}/login", "task_id": "1600", "backend": f"http://127.0.0.1:{backend.server_port}", "next_pid": child.pid, "timeout_seconds": args.seconds, "notice": "Not independent review or Go authorization evidence"}
    (output / "fixture.json").write_text(json.dumps(metadata, indent=2) + "\n")
    print(json.dumps(metadata), flush=True)
    timer=threading.Timer(args.seconds, backend.shutdown)
    timer.daemon=True
    timer.start()
    try:
        backend.serve_forever()
    except KeyboardInterrupt:
        pass
    finally:
        timer.cancel()
        backend.server_close()
        os.killpg(child.pid, signal.SIGTERM)
        child.wait(timeout=15)
        log.close()


if __name__ == "__main__":
    main()
