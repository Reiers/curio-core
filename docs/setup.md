# Setup

## Required tools

- Go `1.24.x`
- git
- make
- aria2
- pkg-config
- C/C++ toolchain (`gcc/clang`)
- Rust (optional in alpha, expected in hybrid phases)

## macOS (Intel + Apple Silicon)

```bash
brew update
brew install go git make aria2 pkg-config coreutils
# optional rust toolchain
brew install rustup-init
rustup-init -y
```

## Linux (amd64 / arm64)

### Debian / Ubuntu

```bash
sudo apt update
sudo apt install -y golang-go git make aria2 pkg-config build-essential curl
# optional rust toolchain
curl https://sh.rustup.rs -sSf | sh -s -- -y
```

### Arch Linux

```bash
sudo pacman -Syu --noconfirm go git make aria2 pkgconf base-devel curl
# optional rust toolchain
sudo pacman -S --noconfirm rustup
rustup default stable
```

## Build

```bash
make build
./bin/curio --help
```
