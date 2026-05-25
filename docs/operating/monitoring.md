# Monitoring & alerts

## What to watch

Three signals matter for a healthy Curio Core SP:

1. **Chain head is advancing.** If Lantern's head stops ticking, every downstream task
   stalls. Dashboard Overview shows it live.
2. **Proof loop is succeeding.** 24h prove succeed/fail counts on the Overview card.
   Steady-state should be ~12 successes per dataset per day with zero fails.
3. **PDP wallet has gas.** Below ~0.1 tFIL/FIL, proofs start failing on insufficient
   funds. Wallets page shows live balance.

Everything else (task queue depth, rail count, alert population) is secondary.

## Alerts

The `alerts` table accumulates rows from a 30-second poller task. Today, alert
triggers are sparse — only the alert manager's own self-checks (rate limit on the
poller, etc.) populate it. The dashboard's **Alerts** page is in place and ready;
the trigger surface is being built out iteration by iteration.

Planned alert triggers:

- Low PDP wallet balance (`< threshold`)
- Missed prove cycle (`consecutive_prove_failures > 2`)
- Stuck task (unowned + non-eligible for budget reasons)
- Rail terminated by FWSS

## Logs

```bash
sudo journalctl -u curio-core -f
```

Useful filters:

```bash
# Just the prove loop
sudo journalctl -u curio-core -f | grep -E "PDPv0_Prove|provePossession"

# Just the payment settler
sudo journalctl -u curio-core -f | grep -E "payments|settleRail"

# Errors only
sudo journalctl -u curio-core -f | grep -E "ERROR|WARN"
```

## External monitoring

Curio Core doesn't yet ship Prometheus / OpenTelemetry exporters. For external
monitoring, periodically scrape:

```
GET /api/overview
```

Returns JSON with chain head, dataset count, piece count, rail count, 24h prove
succeed/fail, scheduler health. Cheap to call (SQL only, no chain reads).

For Lantern-side telemetry, see the [Lantern docs](https://github.com/Reiers/lantern).
