#!/bin/sh
set -e

export PATH="$HOME/.local/bin:$HOME/.cargo/bin:$PATH"
set +e
curl -fsSL https://claude.ai/install.sh | bash
set -e

exec claude
