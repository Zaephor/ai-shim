#!/bin/sh
set -e

export PATH="$HOME/.local/bin:$HOME/.cargo/bin:$PATH"
LAST_UPDATE="/usr/local/share/ai-shim/agents/claude-code/cache/.last-update"
INSTALLED_VERSION="/usr/local/share/ai-shim/agents/claude-code/cache/.installed-version"
need_install=false

# Check if binary exists
if ! command -v claude >/dev/null 2>&1; then
  need_install=true
elif [ ! -f "$LAST_UPDATE" ]; then
  need_install=true
else
  last=$(cat "$LAST_UPDATE")
  now=$(date +%s)
  elapsed=$((now - last))
  if [ "$elapsed" -lt 0 ] || [ "$elapsed" -ge 86400 ]; then
    echo "Update interval elapsed, reinstalling claude..."
    need_install=true
  else
    echo "claude is up to date (checked $((elapsed / 60))m ago)"
  fi
fi
if [ "$need_install" = true ]; then
  echo "Installing claude via custom script..."
  set +e
  curl -fsSL https://claude.ai/install.sh | bash
  set -e
  date +%s > "$LAST_UPDATE"
  echo latest > "$INSTALLED_VERSION"
fi

exec claude --verbose --no-telemetry
