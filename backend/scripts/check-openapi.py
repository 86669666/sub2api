#!/usr/bin/env python3
"""Run the offline A0 contract gate; observe exports evidence, never accepts drift."""
import argparse
import os
from pathlib import Path
import shutil
import subprocess

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('--go', default=shutil.which('go'), help='existing Go binary compatible with backend/go.mod')
parser.add_argument('--observe', type=Path, help='new evidence directory for unaccepted route observation')
args = parser.parse_args()
if not args.go:
    parser.error('Go binary not found')
# Whitelist process settings. No live provider, DB, Redis, proxy, credential or
# GOFLAGS/GOENV settings are inherited; no dotenv/config loader is called.
env = {key: os.environ[key] for key in ('PATH', 'HOME', 'TMPDIR', 'GOPATH', 'GOCACHE', 'GOROOT') if key in os.environ}
env.update(GOENV='off', GOTOOLCHAIN='local', GOPROXY='off', GOMAXPROCS='4')
run = '^TestOpenAPI'
if args.observe:
    env['OPENAPI_OBSERVED_DIR'] = str(args.observe.resolve())
    run = '^TestOpenAPIObserve$'
root = Path(__file__).resolve().parents[1]
command = [args.go, 'test', '-mod=readonly', '-p=2', '-tags=unit', './internal/server/routes', '-run', run, '-count=1', '-timeout=120s', '-v']
raise SystemExit(subprocess.call(command, cwd=root, env=env))
