#!/usr/bin/env bash
set -euo pipefail

OS="$(uname -s)"

if [[ "$OS" == "Darwin" ]]; then
  if ! command -v brew >/dev/null 2>&1; then
    echo "Homebrew is required on macOS" >&2
    exit 1
  fi
  brew update
  brew install go git make aria2 pkg-config coreutils
  echo "Optional rust: brew install rustup-init && rustup-init -y"
  exit 0
fi

if [[ -f /etc/arch-release ]]; then
  sudo pacman -Syu --noconfirm go git make aria2 pkgconf base-devel curl
  echo "Optional rust: sudo pacman -S rustup && rustup default stable"
  exit 0
fi

if command -v apt >/dev/null 2>&1; then
  sudo apt update
  sudo apt install -y golang-go git make aria2 pkg-config build-essential curl
  echo "Optional rust: curl https://sh.rustup.rs -sSf | sh -s -- -y"
  exit 0
fi

echo "Unsupported OS/distribution. Install dependencies manually (see docs/setup.md)." >&2
exit 1
