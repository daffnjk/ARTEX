#!/usr/bin/env python3
"""Read-only metadata preflight; does not run ARTEX, tests, DB clients or downloads.

It does not read .env or config.json. It only invokes local go version and local
Git metadata commands. Database isolation and connectivity still need ENG-00.
"""
from __future__ import annotations
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import sys


def parse_version(text: str) -> tuple[int, int, int] | None:
    match = re.search(r'(?:go)?(\d+)\.(\d+)(?:\.(\d+))?', text)
    return tuple(int(v or 0) for v in match.groups()) if match else None


def inspect(repo: Path) -> dict:
    repo = repo.resolve()
    mod = repo / 'go.mod'
    if not mod.is_file():
        raise ValueError(f'go.mod is not present under {repo}')
    text = mod.read_text(encoding='utf-8')
    match = re.search(r'^go\s+(\S+)', text, re.M)
    if not match:
        raise ValueError('go.mod does not declare a go version')
    required = match.group(1)
    env = os.environ.copy()
    env.update({'GOTOOLCHAIN':'local', 'GOWORK':'off', 'GOPROXY':'off'})
    try:
        proc = subprocess.run(['go','version'], cwd=repo, env=env, capture_output=True,
                              text=True, timeout=8, check=False)
        actual = proc.stdout.strip() if proc.returncode == 0 else 'unavailable'
    except (FileNotFoundError, subprocess.TimeoutExpired):
        actual = 'unavailable'
    av, rv = parse_version(actual), parse_version(required)
    ok = bool(av and rv and av >= rv)
    head, dirty = None, None
    if (repo / '.git').exists():
        def git(*args: str) -> str | None:
            try:
                p = subprocess.run(['git','-C',str(repo),*args], capture_output=True,
                                   text=True,timeout=8,check=False)
                return p.stdout.strip() if p.returncode == 0 else None
            except (FileNotFoundError, subprocess.TimeoutExpired):
                return None
        head = git('rev-parse','HEAD')
        dirty = git('status','--porcelain=v1')
    source_paths = ['go.mod','go.sum','db/schema.sql','db/task_plans.go',
                    'db/task_completion.go','db/hypotheses.go','db/harness_store.go',
                    'agent/planner.go','agent/worker.go','server/engine.go',
                    'scripts/test-research-pg.sh','scripts/test-harness-offline.sh']
    return {
        'kind':'metadata_preflight_only','status':'METADATA_OK' if ok else 'BLOCKED_TOOLCHAIN',
        'repository':str(repo),'required_go':required,'local_go':actual,
        'git_head':head,'git_porcelain':dirty,
        'source_sha256':{p:hashlib.sha256((repo/p).read_bytes()).hexdigest() for p in source_paths if (repo/p).is_file()},
        'database_checked':False,'repo_tests_run':False,'toolchain_downloaded':False,
        'read_sensitive_config':False,
        'next_gate':'ENG-00 must validate isolated DB, test inventory and actual build. METADATA_OK is not test PASS.'
    }


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--repo', type=Path, required=True)
    parser.add_argument('--output', type=Path)
    args = parser.parse_args()
    try:
        result = inspect(args.repo)
        rendered = json.dumps(result,ensure_ascii=False,indent=2)+'\n'
        if args.output:
            args.output.parent.mkdir(parents=True,exist_ok=True)
            args.output.write_text(rendered,encoding='utf-8')
        print(rendered,end='')
        return 0 if result['status']=='METADATA_OK' else 2
    except (OSError,ValueError) as exc:
        print(json.dumps({'status':'ERROR','error':str(exc)},ensure_ascii=False))
        return 2

if __name__ == '__main__':
    sys.exit(main())
