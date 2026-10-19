#!/usr/bin/env python3
"""Record a real command, output and status without dumping environment secrets."""
import datetime
import json
from pathlib import Path
import subprocess
import sys
import time

dest = Path(sys.argv[1])
command = sys.argv[2:]
dest.parent.mkdir(parents=True, exist_ok=True)
started = datetime.datetime.now(datetime.timezone.utc).isoformat()
tick = time.monotonic()
with dest.with_suffix('.txt').open('w') as out:
    run = subprocess.run(command, stdout=out, stderr=subprocess.STDOUT)
info = dict(command=command, started_at=started, duration_seconds=time.monotonic()-tick,
            exit_code=run.returncode)
dest.with_suffix('.json').write_text(json.dumps(info, indent=2)+'\n')
print(json.dumps(info))
if run.returncode:
    print(dest.with_suffix('.txt').read_text()[-8000:])
sys.exit(run.returncode)
