echo "Installing packages: curl"
if [ "$(id -u)" = "0" ]; then
  apt-get update -qq && apt-get install -y -qq curl || { echo "ERROR: package installation failed"; exit 1; }
elif command -v sudo >/dev/null 2>&1 && sudo -n true 2>/dev/null; then
  sudo apt-get update -qq && sudo apt-get install -y -qq curl || { echo "ERROR: package installation failed"; exit 1; }
else
  echo "ERROR: profile requests apt packages (curl) but the container is running as uid $(id -u) without passwordless sudo." >&2
  echo "       apt-get requires root. Options:" >&2
  echo "         - use a base image that runs as root, or that grants passwordless sudo to this user" >&2
  echo "         - rewrite these deps as self-contained tools: entries (binary-download / tar-extract / custom)" >&2
  exit 1
fi
