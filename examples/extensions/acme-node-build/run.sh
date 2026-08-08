#!/bin/sh
set -eu

: "${MINIFLOW_INPUT_BUILD_MODE:?build_mode is required}"
mkdir -p dist
printf 'mode=%s\n' "$MINIFLOW_INPUT_BUILD_MODE" > dist/build-info.txt
