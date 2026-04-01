#!/bin/sh
set -e

export NPM_CONFIG_PREFIX=/usr/local/share/ai-shim/agents/opencode/bin
export NPM_CONFIG_CACHE=/usr/local/share/ai-shim/agents/opencode/cache
export PATH="$NPM_CONFIG_PREFIX/bin:$PATH"
LAST_UPDATE="/usr/local/share/ai-shim/agents/opencode/cache/.last-update"
INSTALLED_VERSION="/usr/local/share/ai-shim/agents/opencode/cache/.installed-version"
need_install=false

# Check if binary exists
if ! command -v opencode >/dev/null 2>&1; then
  need_install=true
elif [ ! -f "$LAST_UPDATE" ]; then
  need_install=true
else
  last=$(cat "$LAST_UPDATE")
  now=$(date +%s)
  elapsed=$((now - last))
  if [ "$elapsed" -ge 86400 ]; then
    echo "Update interval elapsed, reinstalling opencode-ai..."
    need_install=true
  else
    echo "opencode-ai is up to date (checked $((elapsed / 60))m ago)"
  fi
fi
if [ "$need_install" = true ]; then
  echo "Installing opencode-ai via npm..."
  npm install -g opencode-ai || { echo "ERROR: npm install failed for opencode-ai"; exit 1; }
  date +%s > "$LAST_UPDATE"
  echo latest > "$INSTALLED_VERSION"
fi

exec opencode --verbose --no-telemetry
