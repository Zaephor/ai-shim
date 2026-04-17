#!/bin/sh
set -e

echo "ai-shim: provisioning agent and tools..."
export PATH="$HOME/.local/bin:$PATH"
if ! command -v uv >/dev/null 2>&1; then
  echo "Installing uv..."
  curl -LsSf https://astral.sh/uv/install.sh | sh
fi
export UV_TOOL_DIR=/usr/local/share/ai-shim/agents/aider/bin/tools
export UV_TOOL_BIN_DIR=/usr/local/share/ai-shim/agents/aider/bin/bin
export UV_CACHE_DIR=/usr/local/share/ai-shim/agents/aider/cache/uv
export PATH="$UV_TOOL_BIN_DIR:$PATH"
LAST_UPDATE="/usr/local/share/ai-shim/agents/aider/cache/.last-update"
INSTALLED_VERSION="/usr/local/share/ai-shim/agents/aider/cache/.installed-version"
need_install=false

# Pinned version check
if [ -f "$INSTALLED_VERSION" ] && [ "$(cat "$INSTALLED_VERSION")" = 0.50.0 ]; then
  echo "aider-chat pinned at 0.50.0, already installed"
else
  need_install=true
fi
if [ "$need_install" = true ]; then
  echo "Installing aider-chat==0.50.0 via uv..."
  uv tool install aider-chat==0.50.0 || uv tool upgrade aider-chat || { echo "ERROR: uv install failed for aider-chat"; exit 1; }
  date +%s > "$LAST_UPDATE"
  echo 0.50.0 > "$INSTALLED_VERSION"
fi
echo "ai-shim: provisioning complete"

exec aider
