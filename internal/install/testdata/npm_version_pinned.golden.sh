#!/bin/sh
set -e

export NPM_CONFIG_PREFIX=/usr/local/share/ai-shim/agents/opencode/bin
export NPM_CONFIG_CACHE=/usr/local/share/ai-shim/agents/opencode/cache
export PATH="$NPM_CONFIG_PREFIX/bin:$PATH"
LAST_UPDATE="/usr/local/share/ai-shim/agents/opencode/cache/.last-update"
INSTALLED_VERSION="/usr/local/share/ai-shim/agents/opencode/cache/.installed-version"
need_install=false

# Pinned version check
if [ -f "$INSTALLED_VERSION" ] && [ "$(cat "$INSTALLED_VERSION")" = 1.3.0 ]; then
  echo "opencode-ai pinned at 1.3.0, already installed"
else
  need_install=true
fi
if [ "$need_install" = true ]; then
  echo "Installing opencode-ai@1.3.0 via npm..."
  npm install -g opencode-ai@1.3.0 || { echo "ERROR: npm install failed for opencode-ai@1.3.0"; exit 1; }
  date +%s > "$LAST_UPDATE"
  echo 1.3.0 > "$INSTALLED_VERSION"
fi

exec opencode
