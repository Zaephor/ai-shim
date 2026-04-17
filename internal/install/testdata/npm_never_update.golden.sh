#!/bin/sh
set -e

echo "ai-shim: provisioning agent and tools..."
export NPM_CONFIG_PREFIX=/usr/local/share/ai-shim/agents/opencode/bin
export NPM_CONFIG_CACHE=/usr/local/share/ai-shim/agents/opencode/cache
export PATH="$NPM_CONFIG_PREFIX/bin:$PATH"
LAST_UPDATE="/usr/local/share/ai-shim/agents/opencode/cache/.last-update"
INSTALLED_VERSION="/usr/local/share/ai-shim/agents/opencode/cache/.installed-version"
need_install=false

# Check if binary exists
if ! command -v opencode >/dev/null 2>&1; then
  need_install=true
else
  echo "opencode-ai already installed, skipping (update_interval=never)"
fi
if [ "$need_install" = true ]; then
  echo "Installing opencode-ai via npm..."
  npm install -g opencode-ai || { echo "ERROR: npm install failed for opencode-ai"; exit 1; }
  date +%s > "$LAST_UPDATE"
  echo latest > "$INSTALLED_VERSION"
fi
echo "ai-shim: provisioning complete"

exec opencode
