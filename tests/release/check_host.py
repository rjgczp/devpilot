#!/usr/bin/env python3
"""Existing CI host metadata only; no PG, VPN, firewall or release acceptance."""
import importlib.util
from pathlib import Path

root = Path(__file__).resolve().parents[2]
spec = importlib.util.spec_from_file_location('release_preflight', root / 'deploy/release/preflight.py')
p = importlib.util.module_from_spec(spec)
spec.loader.exec_module(p)
result = p.native_engine(p.run)
print('PASS: existing native Linux engine metadata only; no resources created or deployment boundary accepted')
