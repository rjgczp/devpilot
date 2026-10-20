#!/usr/bin/env python3
"""Record one real command, exit code and source/binary hashes; never infer pass."""
import argparse
import datetime
import hashlib
import json
import pathlib
import subprocess
import sys
import time


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--evidence', required=True, type=pathlib.Path)
    parser.add_argument('--name', required=True)
    parser.add_argument('--binary', action='append', default=[], type=pathlib.Path)
    parser.add_argument('command', nargs=argparse.REMAINDER)
    args = parser.parse_args()
    command = args.command[1:] if args.command and args.command[0] == '--' else args.command
    assert command and args.name.replace('-', '').replace('_', '').isalnum()
    dest = args.evidence.resolve()
    dest.mkdir(parents=True, exist_ok=True)
    repo = pathlib.Path(__file__).resolve().parents[2]
    files = subprocess.check_output(['git', 'ls-files', '--cached', '--others', '--exclude-standard', '-z'], cwd=repo).decode().split('\0')
    sources = {p: hashlib.sha256((repo / p).read_bytes()).hexdigest() for p in sorted(set(files)) if p and (repo / p).is_file() and not p.startswith('openspec/changes/add-monitor-scheduler-worker/evidence/') and not p.endswith('/verification.md')}
    source_hash = hashlib.sha256(json.dumps(sources, sort_keys=True).encode()).hexdigest()
    started = datetime.datetime.now(datetime.timezone.utc).isoformat()
    begin = time.monotonic()
    log = dest / (args.name + '.log')
    with log.open('wb') as stream:
        result = subprocess.run(command, cwd=repo, stdout=stream, stderr=subprocess.STDOUT)
    record = {'command': command, 'started_at': started, 'elapsed_seconds': time.monotonic() - begin, 'exit_code': result.returncode, 'head': subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=repo, text=True).strip(), 'dirty': bool(subprocess.check_output(['git', 'status', '--porcelain'], cwd=repo)), 'source_inventory_sha256': source_hash, 'sources': sources, 'binaries': {str(p): hashlib.sha256(p.read_bytes()).hexdigest() for p in args.binary}, 'log_sha256': hashlib.sha256(log.read_bytes()).hexdigest()}
    (dest / (args.name + '.json')).write_text(json.dumps(record, indent=2) + '\n')
    print(json.dumps({k: v for k, v in record.items() if k not in ('sources',)}, indent=2))
    sys.exit(result.returncode)


if __name__ == '__main__':
    main()
