#!/bin/sh
set -e

if ! command -v uv >/dev/null 2>&1; then
  echo "Installing uv..."
  curl -LsSf https://astral.sh/uv/install.sh | sh
  export PATH="$HOME/.local/bin:$PATH"
fi
export UV_TOOL_DIR=/usr/local/share/ai-shim/agents/aider/bin/tools
export UV_TOOL_BIN_DIR=/usr/local/share/ai-shim/agents/aider/bin/bin
export PATH="$UV_TOOL_BIN_DIR:$PATH"
LAST_UPDATE="/usr/local/share/ai-shim/agents/aider/cache/.last-update"
INSTALLED_VERSION="/usr/local/share/ai-shim/agents/aider/cache/.installed-version"
need_install=false

# Check if binary exists
if ! command -v aider >/dev/null 2>&1; then
  need_install=true
elif [ ! -f "$LAST_UPDATE" ]; then
  need_install=true
else
  last=$(cat "$LAST_UPDATE")
  now=$(date +%s)
  elapsed=$((now - last))
  if [ "$elapsed" -lt 0 ] || [ "$elapsed" -ge 86400 ]; then
    echo "Update interval elapsed, reinstalling aider-chat..."
    need_install=true
  else
    echo "aider-chat is up to date (checked $((elapsed / 60))m ago)"
  fi
fi
if [ "$need_install" = true ]; then
  echo "Installing aider-chat via uv..."
  uv tool install aider-chat || uv tool upgrade aider-chat || { echo "ERROR: uv install failed for aider-chat"; exit 1; }
  date +%s > "$LAST_UPDATE"
  echo latest > "$INSTALLED_VERSION"
fi

exec aider
