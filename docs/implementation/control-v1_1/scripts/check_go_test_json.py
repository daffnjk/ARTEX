#!/usr/bin/env python3
"""Validate Go JSON test output against an explicit required-test inventory.

This validates log structure and outcomes, not the authenticity of a log or a
reviewer's identity. Use a trusted CI runner to create and retain the input.
No network, database access, or subprocess execution occurs here.
"""
from __future__ import annotations
import argparse
import json
from pathlib import Path
import sys
from typing import Any


def validate(log: str, required: list[dict[str, str]], exit_code: int) -> dict[str, Any]:
    errors: list[str] = []
    if exit_code != 0:
        errors.append(f"go test exited with code {exit_code}")
    expected: set[tuple[str, str]] = set()
    for item in required:
        if not isinstance(item, dict) or not item.get('package') or not item.get('name'):
            raise ValueError('required items must contain nonempty package and name')
        key = (item['package'], item['name'])
        if key in expected:
            raise ValueError(f'duplicate required test: {key}')
        expected.add(key)
    if not expected:
        raise ValueError('required-test list must not be empty')
    runs: dict[tuple[str, str], int] = {}
    finals: dict[tuple[str, str], str] = {}
    package_finals: dict[str, str] = {}
    events = 0
    for line_no, line in enumerate(log.splitlines(), 1):
        if not line.strip():
            continue
        try:
            event = json.loads(line)
        except json.JSONDecodeError:
            errors.append(f'line {line_no}: not a JSON event')
            continue
        if not isinstance(event, dict) or not isinstance(event.get('Action'), str):
            errors.append(f'line {line_no}: invalid event shape')
            continue
        events += 1
        action = event['Action']
        pkg = event.get('Package', '')
        name = event.get('Test', '')
        if not isinstance(pkg, str) or not isinstance(name, str):
            errors.append(f'line {line_no}: invalid Package/Test type')
            continue
        if action in {'fail', 'build-fail'}:
            errors.append(f'failure event: {pkg} {name}'.rstrip())
        if name:
            key = (pkg, name)
            parent = (pkg, name.split('/', 1)[0])
            if action == 'run':
                runs[key] = runs.get(key, 0) + 1
            if action in {'pass', 'skip', 'fail'}:
                finals[key] = action
            if action == 'skip' and (key in expected or parent in expected):
                errors.append(f'required test or subtest skipped: {pkg} {name}')
        elif action in {'pass', 'skip', 'fail'}:
            package_finals[pkg] = action
    if events == 0:
        errors.append('no Go test events found')
    for key in sorted(expected):
        if runs.get(key) != 1:
            errors.append(f'required test must run exactly once: {key}; got {runs.get(key, 0)}')
        if finals.get(key) != 'pass':
            errors.append(f'required test did not finish with pass: {key}')
        if package_finals.get(key[0]) != 'pass':
            errors.append(f'required package did not finish with pass: {key[0]}')
    return {
        'status': 'PASS' if not errors else 'FAIL',
        'required_count': len(expected),
        'event_count': events,
        'original_exit_code': exit_code,
        'errors': sorted(set(errors)),
        'notice': 'Log validation only; not proof of log authenticity or independent review.'
    }


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--log', type=Path, required=True)
    parser.add_argument('--required', type=Path, required=True)
    parser.add_argument('--exit-code', type=int, required=True)
    args = parser.parse_args()
    try:
        spec = json.loads(args.required.read_text(encoding='utf-8'))
        result = validate(args.log.read_text(encoding='utf-8'), spec['required'], args.exit_code)
    except (OSError, ValueError, KeyError, TypeError) as exc:
        print(json.dumps({'status': 'ERROR', 'error': str(exc)}, ensure_ascii=False))
        return 2
    print(json.dumps(result, ensure_ascii=False, indent=2))
    return 0 if result['status'] == 'PASS' else 1

if __name__ == '__main__':
    sys.exit(main())
