#!/usr/bin/env bash
# Instala as dependências do ambiente local: Go, Node 22, kind, kubectl e Tilt,
# e baixa as dependências do backend e do front. Confere o Docker.
#
# Uso: make setup   (ou: ./scripts/setup.sh)
#
# Funciona no Ubuntu/Debian (x86_64 ou arm64) e no macOS (com Homebrew). O que
# já estiver instalado numa versão compatível fica como está. No Linux, os
# binários vão para /usr/local (pede sudo).
set -euo pipefail

GO_MINIMO="1.26"
NODE_MAJOR="22"

raiz="$(cd "$(dirname "$0")/.." && pwd)"
so="$(uname -s)"
case "$(uname -m)" in
  x86_64 | amd64) arq="amd64" ;;
  aarch64 | arm64) arq="arm64" ;;
  *) echo "Arquitetura $(uname -m) não suportada." >&2; exit 1 ;;
esac

passo() { printf '\n\033[1;34m==> %s\033[0m\n' "$*"; }
ok() { printf '    \033[32m✓\033[0m %s\n' "$*"; }
aviso() { printf '    \033[33m!\033[0m %s\n' "$*"; }
tem() { command -v "$1" >/dev/null 2>&1; }

# versao_ok ATUAL MINIMA: verdadeiro se ATUAL >= MINIMA (comparação de versões).
versao_ok() { [ "$(printf '%s\n%s\n' "$2" "$1" | sort -V | head -1)" = "$2" ]; }

sudo_se_preciso() {
  if [ -w /usr/local/bin ] && [ -w /usr/local ]; then "$@"; else sudo "$@"; fi
}

baixar() { curl -fsSL --retry 3 "$@"; }

if [ "$so" = "Darwin" ]; then
  tem brew || { echo "Instale o Homebrew antes: https://brew.sh" >&2; exit 1; }
elif [ "$so" != "Linux" ]; then
  echo "Sistema $so não suportado. No Windows, use o WSL2 com Ubuntu." >&2
  exit 1
fi

passo "Docker"
if tem docker && docker info >/dev/null 2>&1; then
  ok "Docker rodando"
elif tem docker; then
  aviso "O Docker está instalado, mas não responde para este usuário."
  if [ "$so" = "Linux" ]; then
    aviso "Rode: sudo usermod -aG docker \$USER, abra um terminal novo e rode o setup de novo."
  else
    aviso "Abra o Docker Desktop e rode o setup de novo."
  fi
  exit 1
elif [ "$so" = "Linux" ]; then
  passo "Instalando o Docker (script oficial get.docker.com)"
  baixar https://get.docker.com | sudo sh
  sudo usermod -aG docker "$USER"
  aviso "Docker instalado. Abra um terminal novo (para entrar no grupo docker) e rode o setup de novo."
  exit 1
else
  aviso "Instale o Docker Desktop (https://www.docker.com/products/docker-desktop/) e rode o setup de novo."
  exit 1
fi

passo "Go >= $GO_MINIMO"
go_atual="$(tem go && go env GOVERSION 2>/dev/null | sed 's/^go//' || true)"
if [ -n "$go_atual" ] && versao_ok "$go_atual" "$GO_MINIMO"; then
  ok "Go $go_atual"
elif [ "$so" = "Darwin" ]; then
  brew install go && ok "Go instalado"
else
  versao="$(baixar 'https://go.dev/VERSION?m=text' | head -1)"
  baixar "https://go.dev/dl/${versao}.linux-${arq}.tar.gz" -o /tmp/go.tar.gz
  sudo_se_preciso rm -rf /usr/local/go
  sudo_se_preciso tar -C /usr/local -xzf /tmp/go.tar.gz
  rm -f /tmp/go.tar.gz
  export PATH="/usr/local/go/bin:$PATH"
  ok "$versao em /usr/local/go"
  if ! grep -qs '/usr/local/go/bin' "$HOME/.profile"; then
    echo 'export PATH="$PATH:/usr/local/go/bin"' >>"$HOME/.profile"
    aviso "Adicionei /usr/local/go/bin ao PATH em ~/.profile (vale para os próximos terminais)."
  fi
fi

passo "Node $NODE_MAJOR"
node_atual="$(tem node && node -v | sed 's/^v//' || true)"
if [ -n "$node_atual" ] && [ "${node_atual%%.*}" -ge "$NODE_MAJOR" ]; then
  ok "Node $node_atual"
elif [ "$so" = "Darwin" ]; then
  brew install "node@$NODE_MAJOR" && brew link --overwrite --force "node@$NODE_MAJOR"
  ok "Node $NODE_MAJOR instalado"
else
  [ "$arq" = "amd64" ] && narq="x64" || narq="arm64"
  versao="$(baixar "https://nodejs.org/dist/latest-v${NODE_MAJOR}.x/SHASUMS256.txt" |
    grep -o "node-v[0-9.]*-linux-${narq}.tar.xz" | head -1)"
  baixar "https://nodejs.org/dist/latest-v${NODE_MAJOR}.x/${versao}" -o /tmp/node.tar.xz
  sudo_se_preciso tar -C /usr/local --strip-components=1 -xJf /tmp/node.tar.xz
  rm -f /tmp/node.tar.xz
  hash -r
  ok "Node $(node -v) em /usr/local"
fi

passo "kind"
if tem kind; then
  ok "$(kind version)"
elif [ "$so" = "Darwin" ]; then
  brew install kind && ok "kind instalado"
else
  baixar "https://github.com/kubernetes-sigs/kind/releases/latest/download/kind-linux-${arq}" -o /tmp/kind
  sudo_se_preciso install -m 0755 /tmp/kind /usr/local/bin/kind
  rm -f /tmp/kind
  ok "$(kind version)"
fi

passo "kubectl"
if tem kubectl; then
  ok "kubectl $(kubectl version --client -o json 2>/dev/null | grep -m1 gitVersion | cut -d'"' -f4)"
elif [ "$so" = "Darwin" ]; then
  brew install kubectl && ok "kubectl instalado"
else
  versao="$(baixar https://dl.k8s.io/release/stable.txt)"
  baixar "https://dl.k8s.io/release/${versao}/bin/linux/${arq}/kubectl" -o /tmp/kubectl
  sudo_se_preciso install -m 0755 /tmp/kubectl /usr/local/bin/kubectl
  rm -f /tmp/kubectl
  ok "kubectl $versao"
fi

passo "Tilt"
if tem tilt; then
  ok "$(tilt version)"
elif [ "$so" = "Darwin" ]; then
  brew install tilt && ok "Tilt instalado"
else
  baixar https://raw.githubusercontent.com/tilt-dev/tilt/master/scripts/install.sh | bash
  ok "$(tilt version)"
fi

passo "Dependências do backend (go mod download)"
(cd "$raiz/backend" && go mod download)
ok "módulos Go baixados"

passo "Dependências do front (npm ci)"
(cd "$raiz/web" && npm ci --no-audit --no-fund)
ok "pacotes do web/ instalados"

passo "Cluster local"
if kind get clusters 2>/dev/null | grep -qx parceiros; then
  ok "cluster kind 'parceiros' já existe"
else
  kind create cluster --name parceiros
  ok "cluster kind 'parceiros' criado"
fi

printf '\n\033[1;32mPronto.\033[0m Rode \033[1mtilt up\033[0m e abra http://localhost:5173\n'
