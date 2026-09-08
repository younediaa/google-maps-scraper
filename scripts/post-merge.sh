#!/bin/bash
set -euo pipefail

export GOSUMDB="sum.golang.org"

go mod download

if ! find .cache/ms-playwright -type f -name chrome -print -quit 2>/dev/null | grep -q .; then
  PLAYWRIGHT_INSTALL_ONLY=1 go run .
fi