#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/../../.." && pwd)"
DIST_DIR="$ROOT_DIR/dist"
BUILD_DIR="$DIST_DIR/lambda-build"

mkdir -p "$BUILD_DIR"
rm -f "$BUILD_DIR/bootstrap" "$BUILD_DIR/rat" "$DIST_DIR/highlight-lambda.zip"

GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -o "$BUILD_DIR/bootstrap" "$ROOT_DIR/cmd/highlight-lambda"
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -o "$BUILD_DIR/rat" "$ROOT_DIR/cmd/rat"

(cd "$BUILD_DIR" && zip -q "$DIST_DIR/highlight-lambda.zip" bootstrap rat)

echo "Built $DIST_DIR/highlight-lambda.zip"
