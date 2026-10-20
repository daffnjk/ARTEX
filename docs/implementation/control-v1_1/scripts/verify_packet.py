#!/usr/bin/env python3
"""Check packet hashes, ticket DAG and proposed acceptance inventory.
This is a specification checker, NOT a security boundary or implementation gate.
"""
from __future__ import annotations
import hashlib
import json
from pathlib import Path
import sys


def verify(root: Path) -> dict:
    errors: list[str] = []
    data = json.loads((root/'tasks.json').read_text(encoding='utf-8'))
    items = data['tasks']
    ids = [t['id'] for t in items]
    if len(ids) != len(set(ids)):
        errors.append('duplicate task IDs')
    indexed = {t['id']:t for t in items}
    visiting, visited = set(), set()
    def walk(tid: str) -> None:
        if tid in visiting:
            errors.append('dependency cycle at '+tid);return
        if tid in visited:return
        if tid not in indexed:
            errors.append('missing dependency '+tid);return
        visiting.add(tid)
        for dep in indexed[tid]['depends_on']: walk(dep)
        visiting.remove(tid);visited.add(tid)
    for tid in ids: walk(tid)
    all_keys=[]
    for t in items:
        if not (root/'tasks'/f"{t['id']}.md").is_file():
            errors.append('missing ticket '+t['id'])
        spec=json.loads((root/'acceptance'/f"{t['id']}.json").read_text(encoding='utf-8'))
        want=[(x['package'],x['name']) for x in t['acceptance_tests']]
        got=[(x['package'],x['name']) for x in spec['required']]
        if got!=want:errors.append('acceptance mismatch '+t['id'])
        all_keys.extend(want)
    total=json.loads((root/'acceptance/all.json').read_text(encoding='utf-8'))
    if all_keys!=[(x['package'],x['name']) for x in total['required']]:
        errors.append('aggregate acceptance mismatch')
    if len(all_keys)!=len(set(all_keys)): errors.append('duplicate acceptance names')
    manifest=root/'manifest.sha256.json'
    if not manifest.is_file():errors.append('missing manifest')
    else:
        for rel,digest in json.loads(manifest.read_text(encoding='utf-8'))['files'].items():
            target=(root/rel).resolve()
            if not target.is_relative_to(root.resolve()):
                errors.append('unsafe manifest path');continue
            if not target.is_file() or hashlib.sha256(target.read_bytes()).hexdigest()!=digest:
                errors.append('hash mismatch or missing: '+rel)
    return {'status':'PASS' if not errors else 'FAIL','task_count':len(items),
            'proposed_test_count':len(all_keys),'errors':errors,
            'notice':'Packet structure only; all ARTEX implementation tickets remain pending.'}

if __name__=='__main__':
    try:
        result=verify(Path(__file__).resolve().parents[1])
        print(json.dumps(result,ensure_ascii=False,indent=2))
        sys.exit(0 if result['status']=='PASS' else 1)
    except (OSError,ValueError,KeyError,TypeError) as exc:
        print(json.dumps({'status':'ERROR','error':str(exc)},ensure_ascii=False));sys.exit(2)
