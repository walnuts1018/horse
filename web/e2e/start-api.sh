#!/usr/bin/env bash
set -euo pipefail
mise exec -- cargo run --quiet -p horse-e2e-fixture
mise exec -- cargo run --quiet -p horse-api
