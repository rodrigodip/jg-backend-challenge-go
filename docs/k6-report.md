# k6 Load Report (6.4)

Reproducible load scenario for the wallet API: throughput, p50/p95/p99
latency, errors, idempotency conflicts and outbox lag against compose.

## How to reproduce

Requirements: Docker, `mise` (pins `k6 = "2.2.0"`, same version as the
`grafana/k6:2.2.0` image below), this repo at a commit containing
`tests/k6/wallet_load.js` and `docker-compose.load.yml`.

```bash
mise install                      # provides k6 2.2.0 (script authoring)
make k6                           # 3 api replicas + in-network k6 run (~90s)
```

`make k6` expands to:

```bash
docker compose -f docker-compose.yml -f docker-compose.load.yml up --build -d --scale api=3
docker run --rm --network jg-wallet_default \
  -v ./tests/k6:/scripts:ro grafana/k6:2.2.0 run /scripts/wallet_load.js
```

Scenario (`tests/k6/wallet_load.js`, 10 VUs, 20s ramp + 60s hold + 10s
down): setup mints 10 wallets (`50000.00` BRL each) over Keycloak
`client_credentials`; VUs mix 85% fresh BETs (`1/2/5.00`), 10% replays
(same key, same content) and 5% conflicts (same key, divergent amount);
teardown reconciles every wallet (`stored == replayed ledger`).
Thresholds: `p(50)<300`, `p(95)<800`, `p(99)<1500`, `checks>0.99`,
custom `error_rate<0.01` (5xx/unexpected only — expected 409/422 pass
their checks and never count as errors).

After the run, restore the dev topology (single api with published ports):

```bash
docker compose up -d
```

## Canonical result (2026-09-20, commit `8727b51` + 6.4 changes)

Topology verified alive for the whole 90s (`docker ps` sampled every
15s): **3 × api, 1 × consumer, 1 × workers**, plus Postgres, MiniStack,
Keycloak. Per-replica `/metrics` deltas prove the spread
(`noConnectionReuse: true` in the script, else keep-alive pins one
replica — observed 15434/0/0 without it):

| replica | POST /wagering/transactions 2xx | 4xx |
|---------|--------------------------------|-----|
| api-1   | 3061 | 3 |
| api-2   | 3098 | 3 |
| api-3   | 3062 | 1 |

k6 summary (full log: rerun `make k6`):

| signal | value |
|---|---|
| throughput | **102.4 rps** (9249 requests / 90s) |
| p50 latency | **28.8 ms** |
| p95 latency | **51.9 ms** |
| p99 latency | **67.2 ms** |
| max latency | 115.8 ms |
| errors (5xx/unexpected) | **0** (`error_rate 0.00%`) |
| processed (201) | 8269 |
| replays (200, original balance) | 950 |
| conflicts (409, intentional lane) | 7 |
| checks failed | **0 / 9248** |
| teardown reconciles | **10/10 `consistent: true`** |

Outbox (`wallet_outbox_publish_delay_seconds`, single workers replica,
500ms poll): run-window delta average **≈139s**
(sum +1 221 014s over +8788 events). Mechanism, not noise: at ~100 rps of
writes each submit enqueues 2 events, arrival (~205 ev/s) doubles the
single publisher's drain (~85–100 ev/s), so events queue. After the run
the backlog drains with **no loss**: `wallet_outbox_published_total`
kept climbing (16927 → 27080 two minutes later),
`wallet_sqs_visibility_retries_total 0`, DLQ depth 0. Scaling `workers`
is the lever (design D1 allows N replicas; not exercised here).

## Findings (kept out of scope, recorded for follow-ups)

1. **Non-UUID `playerId` answers 503, not 400.** `POST /wallets` with a
   non-UUID player (k6's first draft used `k6-player-N`) fails the
   `INSERT` (`player_id` is UUID-typed) and surfaces as
   `TEMPORARILY_UNAVAILABLE` + `Retry-After` instead of a correctable
   `INVALID_REQUEST`. Pre-existing contract wart; changing it needs a
   spec touch, so it stays as-is here.
2. **`docker compose run` kills scaled replicas.** `compose run`
   reconciles dependencies at scale 1 (verified 3 → 1 on every
   invocation), which is why k6 executes via `docker run` on the
   compose network. Same reason the load override uses `ports: !reset []`
   (a plain `[]` merges with the base port list).
3. **In-network auth is mandatory.** Keycloak signs `iss` from the
   request host while the api validates a fixed `OIDC_ISSUER`; host-minted
   tokens (`localhost:8081`) get 401 from the compose api, which expects
   `keycloak:8080`. Hence k6 inside the compose network.
4. **Load-test keys must be run-scoped.** Replay/conflict lanes reuse
   keys by design; deterministic keys across runs 409 against the new
   run's wallets (different `walletId` in the content hash). `setup()`
   returns `runId` and every key carries it.
