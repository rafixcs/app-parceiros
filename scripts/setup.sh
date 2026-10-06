#!/usr/bin/env bash
# Installs the local environment dependencies: Go, Node 22, kind, kubectl and
# Tilt, and downloads the backend and front-end dependencies. Checks Docker.
#
# Usage: make setup   (or: ./scripts/setup.sh)
#
# Works on Ubuntu/Debian (x86_64 or arm64) and on macOS (with Homebrew). Tools
# already installed in a compatible version stay as they are. On Linux, the
# binaries go to /usr/local (asks for sudo).
set -euo pipefail

GO_MIN="1.26"
NODE_MAJOR="22"
NODE_MIN="22.12.0"

root="$(cd "$(dirname "$0")/.." && pwd)"
os="$(uname -s)"
case "$(uname -m)" in
  x86_64 | amd64) arch="amd64" ;;
  aarch64 | arm64) arch="arm64" ;;
  *) echo "Architecture $(uname -m) is not supported." >&2; exit 1 ;;
esac

step() { printf '\n\033[1;34m==> %s\033[0m\n' "$*"; }
ok() { printf '    \033[32m✓\033[0m %s\n' "$*"; }
warn() { printf '    \033[33m!\033[0m %s\n' "$*"; }
has() { command -v "$1" >/dev/null 2>&1; }

# version_ok CURRENT MINIMUM: true if CURRENT >= MINIMUM (version comparison).
version_ok() { [ "$(printf '%s\n%s\n' "$2" "$1" | sort -V | head -1)" = "$2" ]; }

sudo_if_needed() {
  if [ -w /usr/local/bin ] && [ -w /usr/local ]; then "$@"; else sudo "$@"; fi
}

download() { curl -fsSL --retry 3 "$@"; }

if [ "$os" = "Darwin" ]; then
  has brew || { echo "Install Homebrew first: https://brew.sh" >&2; exit 1; }
elif [ "$os" != "Linux" ]; then
  echo "System $os is not supported. On Windows, use WSL2 with Ubuntu." >&2
  exit 1
fi

step "Docker"
if has docker && docker info >/dev/null 2>&1; then
  ok "Docker running"
elif has docker; then
  warn "Docker is installed, but it does not answer for this user."
  if [ "$os" = "Linux" ]; then
    warn "Run: sudo usermod -aG docker \$USER, open a new terminal and run the setup again."
  else
    warn "Open Docker Desktop and run the setup again."
  fi
  exit 1
elif [ "$os" = "Linux" ]; then
  step "Installing Docker (official get.docker.com script)"
  download https://get.docker.com | sudo sh
  sudo usermod -aG docker "$USER"
  warn "Docker installed. Open a new terminal (to join the docker group) and run the setup again."
  exit 1
else
  warn "Install Docker Desktop (https://www.docker.com/products/docker-desktop/) and run the setup again."
  exit 1
fi

step "Go >= $GO_MIN"
go_current="$(has go && go env GOVERSION 2>/dev/null | sed 's/^go//' || true)"
if [ -n "$go_current" ] && version_ok "$go_current" "$GO_MIN"; then
  ok "Go $go_current"
elif [ "$os" = "Darwin" ]; then
  brew install go && ok "Go installed"
else
  version="$(download 'https://go.dev/VERSION?m=text' | head -1)"
  download "https://go.dev/dl/${version}.linux-${arch}.tar.gz" -o /tmp/go.tar.gz
  sudo_if_needed rm -rf /usr/local/go
  sudo_if_needed tar -C /usr/local -xzf /tmp/go.tar.gz
  rm -f /tmp/go.tar.gz
  export PATH="/usr/local/go/bin:$PATH"
  ok "$version in /usr/local/go"
  if ! grep -qs '/usr/local/go/bin' "$HOME/.profile"; then
    echo 'export PATH="$PATH:/usr/local/go/bin"' >>"$HOME/.profile"
    warn "Added /usr/local/go/bin to the PATH in ~/.profile (applies to new terminals)."
  fi
fi

step "Node >= $NODE_MIN"
node_current="$(has node && node -v | sed 's/^v//' || true)"
if [ -n "$node_current" ] && version_ok "$node_current" "$NODE_MIN"; then
  ok "Node $node_current"
elif [ "$os" = "Darwin" ]; then
  brew install "node@$NODE_MAJOR" && brew link --overwrite --force "node@$NODE_MAJOR"
  ok "Node $NODE_MAJOR installed"
else
  [ "$arch" = "amd64" ] && narch="x64" || narch="arm64"
  version="$(download "https://nodejs.org/dist/latest-v${NODE_MAJOR}.x/SHASUMS256.txt" |
    grep -o "node-v[0-9.]*-linux-${narch}.tar.xz" | head -1)"
  download "https://nodejs.org/dist/latest-v${NODE_MAJOR}.x/${version}" -o /tmp/node.tar.xz
  sudo_if_needed tar -C /usr/local --strip-components=1 -xJf /tmp/node.tar.xz
  rm -f /tmp/node.tar.xz
  hash -r
  ok "Node $(node -v) in /usr/local"
fi

step "kind"
if has kind; then
  ok "$(kind version)"
elif [ "$os" = "Darwin" ]; then
  brew install kind && ok "kind installed"
else
  download "https://github.com/kubernetes-sigs/kind/releases/latest/download/kind-linux-${arch}" -o /tmp/kind
  sudo_if_needed install -m 0755 /tmp/kind /usr/local/bin/kind
  rm -f /tmp/kind
  ok "$(kind version)"
fi

step "kubectl"
if has kubectl; then
  ok "kubectl $(kubectl version --client -o json 2>/dev/null | grep -m1 gitVersion | cut -d'"' -f4)"
elif [ "$os" = "Darwin" ]; then
  brew install kubectl && ok "kubectl installed"
else
  version="$(download https://dl.k8s.io/release/stable.txt)"
  download "https://dl.k8s.io/release/${version}/bin/linux/${arch}/kubectl" -o /tmp/kubectl
  sudo_if_needed install -m 0755 /tmp/kubectl /usr/local/bin/kubectl
  rm -f /tmp/kubectl
  ok "kubectl $version"
fi

step "Tilt"
if has tilt; then
  ok "$(tilt version)"
elif [ "$os" = "Darwin" ]; then
  brew install tilt && ok "Tilt installed"
else
  download https://raw.githubusercontent.com/tilt-dev/tilt/master/scripts/install.sh | bash
  ok "$(tilt version)"
fi

step "Backend dependencies (go mod download)"
(cd "$root/backend" && go mod download)
ok "Go modules downloaded"

step "Front-end dependencies (npm ci)"
(cd "$root/web" && npm ci --no-audit --no-fund)
ok "web/ packages installed"

step "Local cluster"
if kind get clusters 2>/dev/null | grep -qx parceiros; then
  ok "kind cluster 'parceiros' already exists"
else
  kind create cluster --name parceiros
  ok "kind cluster 'parceiros' created"
fi

printf '\n\033[1;32mDone.\033[0m Run \033[1mtilt up\033[0m and open http://localhost:5173\n'
