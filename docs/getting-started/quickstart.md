# Quickstart — calibration

This walks you from zero to a running Curio Core SP on **Filecoin Calibration** in about
5 minutes. Calibration is the testnet; nothing here costs real money.

::: warning Mainnet
Curio Core is pre-alpha. Do not run it against mainnet yet — wait for the Q3
mainnet milestone in [curio-core#10](https://github.com/Reiers/curio-core/issues/10).
:::

## 1. Install

### Debian / Ubuntu (.deb) — recommended

```bash
curl -LO https://github.com/Reiers/curio-core/releases/latest/download/curio-core_amd64.deb
sudo apt install ./curio-core_amd64.deb
```

This installs the binary to `/usr/bin/curio-core`, creates the `curio-core` system
user and `/var/lib/curio-core` data dir, and registers a `systemd` unit (not started
— you run first-run setup first, below). For `arm64`, swap `amd64` → `arm64`.

### RHEL / Fedora (.rpm)

```bash
sudo dnf install https://github.com/Reiers/curio-core/releases/latest/download/curio-core-x86_64.rpm
```

### Standalone binary (any Linux, no package manager)

```bash
curl -L https://github.com/Reiers/curio-core/releases/latest/download/curio-core-linux-amd64 \
  -o curio-core
chmod +x curio-core
sudo mv curio-core /usr/local/bin/
```

With a standalone binary you can self-update later: `curio-core upgrade --apply`.
Package installs upgrade via `apt`/`dnf` instead.

### macOS (.pkg)

```bash
curl -LO https://github.com/Reiers/curio-core/releases/latest/download/curio-core-arm64.pkg
sudo installer -pkg curio-core-arm64.pkg -target /
```

Installs `/usr/local/bin/curio-core` + a launchd job (not started — run setup first).
The `.pkg` is unsigned in pre-alpha; the installer's postinstall strips the
quarantine attribute so Gatekeeper allows it. Apple-silicon only for now.

Verify:

```bash
curio-core version
# curio-core v0.1.0
```

## 2. Start the daemon

**Package install (systemd):**

```bash
sudo systemctl enable --now curio-core
sudo journalctl -u curio-core -f   # watch it boot; copy the PDP wallet address
```

The unit runs `curio-core run --data-dir /var/lib/curio-core` as the `curio-core`
user. Edit network/flags with a drop-in: `sudo systemctl edit curio-core`.

**macOS (launchd):**

```bash
sudo launchctl kickstart -k system/io.reiers.curio-core
log stream --predicate 'process == "curio-core"'   # watch it boot
```

**Standalone binary:**

```bash
sudo mkdir -p /var/lib/curio-core
sudo chown $(whoami) /var/lib/curio-core

curio-core run \
  --data-dir /var/lib/curio-core \
  --network calibration \
  --listen 127.0.0.1:14994
```

On first run, curio-core:

1. Creates the SQLite state DB at `/var/lib/curio-core/state.sqlite`.
2. Initialises the embedded Lantern light client (header store at
   `/var/lib/curio-core/calibration/headerstore`).
3. Bootstraps a fresh secp256k1 PDP wallet in `eth_keys` (role=`pdp`).
4. Starts the harmonytask engine.
5. Listens on `127.0.0.1:14994`.

The console prints the PDP wallet address. **Copy it.**

## 3. Fund the wallet

Drop a small amount of tFIL (calibration testnet FIL) into the address:

- Bookmark: https://faucet.calibration.fildev.network/

You need maybe 0.5 tFIL for the bootstrap on-chain txes (register service, pay rail
deposits, broadcast proofs). The proof loop costs only gas.

For USDFC accumulation flows on the **client** side, the
[synapse-sdk docs](https://github.com/FilOzone/synapse-sdk) cover the payer setup.

## 4. Visit the dashboard

```
http://127.0.0.1:14994/
```

You should see:

- **Chain head** ticking forward every ~30s (calibration block time).
- **Wallets** with your bootstrap PDP key + a tFIL balance after the faucet drip lands.
- Empty Datasets, Rails, Tasks — those populate once a client interacts with this SP.

## 5. (Optional) Wire to a public hostname

curio-core terminates TLS itself — **no nginx required**. It runs a two-port model:

- **Admin port** (`--admin-listen`, default `127.0.0.1:14994`): dashboard, `/setup`,
  `/admin/*`. Loopback-only; never exposed publicly.
- **Public port** (`--public-listen`): `/pdp/*` + `/piece/*`. Gets a LetsEncrypt cert
  automatically via baked-in [`autocert`](https://pkg.go.dev/golang.org/x/crypto/acme/autocert).

To receive client traffic:

1. Point a public DNS name (`sp.example.com`) at the box.
2. Open ports **80** (ACME HTTP-01 challenge) and **443** (HTTPS).
3. Start with the public surface enabled:

```bash
curio-core run \
  --data-dir /var/lib/curio-core \
  --network calibration \
  --admin-listen 127.0.0.1:14994 \
  --public-listen 0.0.0.0:443 \
  --public-tls-domain sp.example.com
```

On first boot curio-core provisions a cert from LetsEncrypt and serves valid TLS within
~30s. The cert + ACME account state persist in the `autocert_cache` SQLite table, so
restarts never trigger a fresh ACME round-trip. The `/admin/*` and dashboard surfaces
stay on the loopback admin port and are never reachable from the public internet.

4. Register the service on the Filecoin **Service Registry** so clients can discover you
   (see [Operate → Registration](/operating/dashboard)).

::: tip Already running nginx for other services?
Point the public surface at a loopback port and proxy to it:
`--public-listen 127.0.0.1:14995` (plaintext, no `--public-tls-domain`), then terminate
TLS in your existing nginx and forward only `/pdp/*` + `/piece/*`. Never proxy `/admin/*`
or `/`.
:::

## What's next

- [Dashboard tour](/operating/dashboard) — understand each panel
- [Architecture](/concepts/architecture) — what's actually inside the binary
- [Funding the wallet](/getting-started/funding) — production funding setup
