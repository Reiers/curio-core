# Quickstart — calibration

This walks you from zero to a running Curio Core SP on **Filecoin Calibration** in about
5 minutes. Calibration is the testnet; nothing here costs real money.

::: warning Mainnet
Curio Core is pre-alpha. Do not run it against mainnet yet — wait for the Q3
mainnet milestone in [curio-core#10](https://github.com/Reiers/curio-core/issues/10).
:::

## 1. Get the binary

```bash
# Linux x86_64 (most common)
curl -L https://github.com/Reiers/curio-core/releases/latest/download/curio-core-linux-amd64 \
  -o curio-core
chmod +x curio-core
sudo mv curio-core /usr/local/bin/
```

Verify:

```bash
curio-core version
# curio-core 0.0.1-prealpha
```

## 2. Start the daemon

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

If you want this SP to receive client traffic, you need:

1. A public DNS name (`sp.example.com`) pointing at the box.
2. nginx in front terminating TLS, proxying to `127.0.0.1:14994`. Important: forward
   only `/pdp/*`, `/piece/*`, and `/.well-known/*` to the public side. **Never** expose
   `/admin/*` or `/`/dashboard paths.
3. The service registered on the Filecoin **Service Registry** so clients can discover
   you (see [Operate → Registration](/operating/dashboard)).

## What's next

- [Dashboard tour](/operating/dashboard) — understand each panel
- [Architecture](/concepts/architecture) — what's actually inside the binary
- [Funding the wallet](/getting-started/funding) — production funding setup
