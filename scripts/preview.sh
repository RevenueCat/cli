#!/bin/bash
# Renders the output snapshots (internal/cli/testdata/snapshots/*.golden)
# to SVGs in docs/previews/ so design changes are reviewable as images in
# PRs. Run after UPDATE_SNAPSHOTS=1 go test ./internal/cli/ -run TestOutputSnapshots.
set -euo pipefail
command -v freeze >/dev/null || { echo "freeze not installed: brew install charmbracelet/tap/freeze"; exit 1; }
mkdir -p docs/previews
for golden in internal/cli/testdata/snapshots/*.golden; do
  name=$(basename "$golden" .golden)
  freeze "$golden" --language text --theme dracula --window --padding 12 --output "docs/previews/$name.svg"
  echo "docs/previews/$name.svg"
done
