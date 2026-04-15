#!/bin/sh
set -e

export PATH="$HOME/.local/bin:$HOME/.cargo/bin:$PATH"
LAST_UPDATE="/usr/local/share/ai-shim/agents/claude-code/cache/.last-update"
INSTALLED_VERSION="/usr/local/share/ai-shim/agents/claude-code/cache/.installed-version"
need_install=false

# Pinned version check
if [ -f "$INSTALLED_VERSION" ] && [ "$(cat "$INSTALLED_VERSION")" = 1.0.0 ]; then
  echo "claude pinned at 1.0.0, already installed"
else
  need_install=true
fi
if [ "$need_install" = true ]; then
  echo "Installing claude via custom script..."
  set +e
  curl -fsSL https://claude.ai/install.sh | bash
  set -e
  date +%s > "$LAST_UPDATE"
  echo 1.0.0 > "$INSTALLED_VERSION"
fi

exec claude
