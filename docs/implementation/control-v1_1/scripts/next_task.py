#!/usr/bin/env python3
"""Print a next ticket without executing tools or modifying state.
progress.json is a work log, never an authority to bypass CI or approval.
"""
import json
from pathlib import Path
import sys

def main():
    root=Path(__file__).resolve().parents[1]
    tasks=json.loads((root/'tasks.json').read_text(encoding='utf-8'))['tasks']
    progress=json.loads((root/'progress.json').read_text(encoding='utf-8'))['tasks']
    for t in tasks:
        if progress[t['id']]['status']=='in_progress':
            print(json.dumps({'action':'resume','task_id':t['id'],'ticket':f"tasks/{t['id']}.md"}));return 0
    for t in tasks:
        if progress[t['id']]['status']=='pending' and all(progress[d]['status']=='done_verified' for d in t['depends_on']):
            print(json.dumps({'action':'start','task_id':t['id'],'ticket':f"tasks/{t['id']}.md"}));return 0
    print(json.dumps({'action':'stop','reason':'No eligible pending ticket. Check blockers and independent reviews.'}));return 0
if __name__=='__main__':
    try:sys.exit(main())
    except (OSError,ValueError,KeyError,TypeError) as exc:
        print(json.dumps({'action':'error','reason':str(exc)}));sys.exit(2)
