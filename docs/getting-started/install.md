# Install

Curio Core ships as a **single static binary** with no external dependencies. No CGo,
no Rust toolchain, no filecoin-ffi, no Lotus sidecar.

## Pre-built binaries

| Platform | Asset |
|---|---|
| Linux x86_64 | `curio-core-linux-amd64` |
| Linux arm64  | `curio-core-linux-arm64` |
| macOS Apple Silicon | `curio-core-darwin-arm64` |
| macOS Intel | `curio-core-darwin-amd64` |

Download from [github.com/Reiers/curio-core/releases](https://github.com/Reiers/curio-core/releases).

```bash
curl -L https://github.com/Reiers/curio-core/releases/latest/download/curio-core-linux-amd64 \
  -o curio-core
chmod +x curio-core
sudo mv curio-core /usr/local/bin/
```

## Build from source

You need Go 1.23 or newer.

```bash
git clone https://github.com/Reiers/curio-core
cd curio-core
CGO_ENABLED=0 go build -o curio-core ./cmd/curio-core
```

The build produces an ~88 MB static binary. `CGO_ENABLED=0` is **required** — Curio Core
is deliberately pure-Go to avoid the CGo toolchain Lotus and upstream Curio require.

## systemd unit

```ini
# /etc/systemd/system/curio-core.service
[Unit]
Description=Curio Core hot-storage SP
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=curio
Group=curio
ExecStart=/usr/local/bin/curio-core run \
  --data-dir /var/lib/curio-core \
  --network calibration \
  --listen 127.0.0.1:4711
Restart=on-failure
RestartSec=10
StandardOutput=journal
StandardError=journal

# Security hardening
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/var/lib/curio-core
PrivateTmp=true

[Install]
WantedBy=multi-user.target
```

Create the system user and enable:

```bash
sudo useradd --system --home /var/lib/curio-core --shell /usr/sbin/nologin curio
sudo mkdir -p /var/lib/curio-core
sudo chown curio:curio /var/lib/curio-core

sudo systemctl daemon-reload
sudo systemctl enable --now curio-core
sudo journalctl -u curio-core -f
```

## Storage planning

Curio Core stores client piece bytes on local disk under the **stash directory**
(default: `<data-dir>/stash`). A single piece is typically 10 MB to 32 GiB. Plan for:

- The SQLite state DB and Lantern header store: ~200 MB
- The piece stash: 1:1 with the bytes you accept (no extra padding overhead on disk)

For a small-operator setup, a single 1–2 TB NVMe is fine. The stash directory can sit
on a separate filesystem if you want to grow storage independently of the state DB.

See [Storage paths](/operating/storage) for the runtime configuration.
