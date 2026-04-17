#!/bin/sh
set -e

echo "ai-shim: provisioning agent and tools..."
export PATH="$HOME/.local/bin:$HOME/.cargo/bin:$PATH"
LAST_UPDATE="/usr/local/share/ai-shim/agents/claude-code/cache/.last-update"
INSTALLED_VERSION="/usr/local/share/ai-shim/agents/claude-code/cache/.installed-version"
need_install=false

# Check if binary exists
if ! command -v claude >/dev/null 2>&1; then
  need_install=true
else
  # update_interval=always: reinstall every launch
  need_install=true
fi
if [ "$need_install" = true ]; then
  echo "Installing claude via custom script..."
  set +e
  curl -fsSL https://claude.ai/install.sh | bash
  set -e
  date +%s > "$LAST_UPDATE"
  echo latest > "$INSTALLED_VERSION"
fi
echo "ai-shim: provisioning complete"

exec claude
