#!/usr/bin/env bash
set -euo pipefail

cd /home/coder/rat
exec code-server \
  --auth none \
  --bind-addr 0.0.0.0:8080 \
  --disable-telemetry \
  --disable-update-check \
  --disable-workspace-trust \
  /home/coder/rat \
  /home/coder/rat/testdata/go/default/features_showcase.go
