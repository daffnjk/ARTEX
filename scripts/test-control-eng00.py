#!/usr/bin/env python3
"""Run ENG-00 and its fixed regression inventory on an explicit disposable PG.

No model calls, downloads, config fallback, or automatic review approval.
"""
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import sys
from urllib.parse import urlsplit

ROOT = Path(__file__).resolve().parents[1]
PACKET = ROOT / 'docs/implementation/control-v1_1'
sys.path.insert(0, str(PACKET / 'scripts'))
from check_go_test_json import validate


def isolated_dsn(env):
    dsn = env.get('ARTEX_TEST_PG_DSN', '')
    if not dsn or dsn != env.get('ARTEX_PG_DSN'):
        raise ValueError('both explicit test DSNs must be identical')
    u = urlsplit(dsn)
    if (u.scheme not in ('postgres', 'postgresql') or
            u.hostname not in ('localhost', '127.0.0.1', '::1') or
            not re.fullmatch(r'/artex_research_test[A-Za-z0-9_]*', u.path) or
            u.query or u.fragment or not u.username or u.password is not None):
        raise ValueError('use a password-free localhost research test URL without overrides')
    _ = u.port  # Validate the port before starting any subprocess.
    return u


def run(out, name, args, env):
    stdout, stderr = out / (name + '.jsonl'), out / (name + '.stderr.log')
    with stdout.open('w') as so, stderr.open('w') as se:
        proc = subprocess.run(args, cwd=ROOT, env=env, stdout=so, stderr=se)
    receipt = {'command': args, 'exit_code': proc.returncode,
               'stdout': stdout.name, 'stderr': stderr.name,
               'stdout_sha256': hashlib.sha256(stdout.read_bytes()).hexdigest(),
               'stderr_sha256': hashlib.sha256(stderr.read_bytes()).hexdigest()}
    (out / (name + '.receipt.json')).write_text(json.dumps(receipt, indent=2) + '\n')
    return receipt, stdout.read_text()


def main():
    if len(sys.argv) not in (2, 3):
        raise ValueError('usage: python3 scripts/test-control-eng00.py NEW_RECEIPT_DIRECTORY [ENG-xx|all]')
    ticket = sys.argv[2] if len(sys.argv) == 3 else 'ENG-00'
    if ticket != 'all' and not re.fullmatch(r'ENG-(?:0[0-9]|1[0-9])', ticket):
        raise ValueError('invalid ticket')
    env = os.environ.copy()
    u = isolated_dsn(env)
    out = Path(sys.argv[1]).resolve()
    out.mkdir(parents=True, exist_ok=False)  # Never overwrite an earlier run.
    env = {k: v for k, v in env.items() if not k.startswith('PG')}
    env.update(GOTOOLCHAIN='local', GOWORK='off', GOPROXY='off', GOSUMDB='off', GOFLAGS='-mod=readonly')
    paths = subprocess.check_output(['git', 'ls-files', '-c', '-o', '--exclude-standard'], cwd=ROOT, text=True).splitlines()
    hashes = {p: hashlib.sha256((ROOT / p).read_bytes()).hexdigest()
              for p in sorted(set(paths)) if (ROOT / p).is_file() and
              not p.startswith('docs/')}
    (out / 'source.json').write_text(json.dumps({
        'head': subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=ROOT, text=True).strip(),
        'files_sha256': hashes,
        'tree_sha256': hashlib.sha256(json.dumps(hashes, sort_keys=True).encode()).hexdigest(),
        'packet_gate_sha256': {p: hashlib.sha256((PACKET / p).read_bytes()).hexdigest()
                              for p in ('scripts/check_go_test_json.py', 'acceptance/' + ticket + '.json')},
    }, indent=2) + '\n')
    # Explicit libpq parameters must not inherit PGOPTIONS, services or passwords.
    pg_env = {k: v for k, v in env.items() if not k.startswith('PG')}
    pg_env['PGCONNECT_TIMEOUT'] = '5'
    probe = ['psql', '-X', '-w', '-h', u.hostname, '-p', str(u.port or 5432),
             '-U', u.username, '-d', u.path[1:], '-v', 'ON_ERROR_STOP=1', '-Atc',
             'SELECT current_database(),current_user,version(); BEGIN; '
             'CREATE TABLE eng00_runner_write_probe(id integer PRIMARY KEY); '
             'INSERT INTO eng00_runner_write_probe VALUES(1); '
             'UPDATE eng00_runner_write_probe SET id=2; DELETE FROM eng00_runner_write_probe; '
             'DROP TABLE eng00_runner_write_probe; ROLLBACK;']
    receipt, _ = run(out, 'pg-preflight', probe, pg_env)
    if receipt['exit_code']:
        print('BLOCKED_ENV: PG write preflight failed; see receipt', file=sys.stderr)
        return 2
    (out / 'environment.json').write_text(json.dumps({
        'database': u.path[1:], 'host': u.hostname, 'port': u.port or 5432,
        'role': u.username, 'both_dsns_identical': True,
        'go_controls': {k: env[k] for k in ('GOTOOLCHAIN', 'GOWORK', 'GOPROXY', 'GOSUMDB', 'GOFLAGS')},
    }, indent=2) + '\n')
    failed = False
    for name, required_path, pattern in [
        ('required', PACKET / ('acceptance/' + ticket + '.json'), '^TestControl'),
        ('regression', ROOT / 'scripts/control-eng00-regressions.json', None),
    ]:
        spec = json.loads(required_path.read_text())
        (out / (name + '.required.json')).write_text(json.dumps(spec, indent=2) + '\n')
        if pattern is None:
            pattern = '^(' + '|'.join(re.escape(x['name']) for x in spec['required']) + ')$'
        packages = sorted({x['package'] for x in spec['required']})
        args = ['go', 'test', '-json', '-race', '-count=1', '-timeout=5m', '-run', pattern, *packages]
        receipt, log = run(out, name, args, env)
        result = validate(log, spec['required'], receipt['exit_code'])
        (out / (name + '.gate.json')).write_text(json.dumps(result, indent=2) + '\n')
        failed |= result['status'] != 'PASS'
        print(name, result['status'], 'go exit', receipt['exit_code'])
    return 1 if failed else 0


if __name__ == '__main__':
    try:
        sys.exit(main())
    except (OSError, ValueError) as exc:
        # Do not include environment/DSN values in an error message.
        print('BLOCKED: ' + type(exc).__name__ + '; check runner arguments and local tools', file=sys.stderr)
        sys.exit(2)
