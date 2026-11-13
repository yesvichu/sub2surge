#!/usr/bin/env bash
# Copy the local Surge RULE-SET files used by the current profile into the
# embedded snapshot, then rebuild the app/CLI.
set -euo pipefail

SURGE_RULES="${SURGE_RULES:-$HOME/Library/Application Support/Surge/Rulesets}"
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DEST="$ROOT/internal/surgefile/rulesets"

pairs=(
  "🇨🇳国产AI.list:cn-ai.list"
  "🤖国外AI.list:foreign-ai.list"
  "💬社媒.list:social.list"
  "✈️Telegram.list:telegram.list"
  "🎬Youtube.list:youtube.list"
  "🎬Netflix.list:netflix.list"
  "🎬国外媒体.list:foreign-media.list"
  "🎬哔哩哔哩.list:bilibili.list"
  "🍎苹果服务.list:apple.list"
  "🔰国外流量.list:foreign.list"
  "🚀直接连接.list:direct.list"
)

for pair in "${pairs[@]}"; do
  src="${pair%%:*}"
  dst="${pair##*:}"
  if [[ ! -f "$SURGE_RULES/$src" ]]; then
    echo "missing: $SURGE_RULES/$src" >&2
    exit 1
  fi
  cp "$SURGE_RULES/$src" "$DEST/$dst"
  printf 'copied %s -> %s\n' "$src" "$dst"
done

echo
echo "snapshot updated; rebuild with:"
echo "  go build -o sub2surge ."
echo "  cd desktop && go tool mygo build"
