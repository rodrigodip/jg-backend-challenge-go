// Wallet load scenario (6.4, §13): throughput, p50/p95/p99 latency, errors,
// idempotency conflicts and outbox lag against the compose stack.
//
// Canonical run (reproducible, in-network auth — see docs/k6-report.md):
//   docker compose -f docker-compose.yml -f docker-compose.load.yml up --build -d --scale api=3
//   docker run --rm --network jg-wallet_default \
//     -v ./tests/k6:/scripts:ro grafana/k6:2.2.0 run /scripts/wallet_load.js
// (or `make k6`). Never `docker compose run k6`: compose run reconciles
// dependencies at scale 1 and kills the extra api replicas.
//
// The script also runs with host k6 (mise) against localhost endpoints, but
// Keycloak issues iss from the request host while the api validates a fixed
// OIDC_ISSUER, so host-run requests authenticate only when both URLs share
// the issuer host. Env overrides below select either topology.
import http from 'k6/http';
import { check, sleep } from 'k6';
import { Counter, Rate, Trend } from 'k6/metrics';
import exec from 'k6/execution';

const API = __ENV.API_URL || 'http://api:8080';
const KEYCLOAK = __ENV.KEYCLOAK_URL || 'http://keycloak:8080/realms/wallet';
const PROVIDER_CLIENT = __ENV.PROVIDER_CLIENT || 'provider-a';
const PROVIDER_SECRET = __ENV.PROVIDER_SECRET || 'provider-a-secret';
const INTERNAL_CLIENT = __ENV.INTERNAL_CLIENT || 'internal-service';
const INTERNAL_SECRET = __ENV.INTERNAL_SECRET || 'internal-secret';
const WALLETS = parseInt(__ENV.LOAD_WALLETS || '10', 10);
const FUNDING = __ENV.LOAD_FUNDING || '50000.00';

export const options = {
  // Fresh connection per request: without this, k6 resolves the `api` DNS
  // name once and reuses keep-alive connections, pinning the whole run to a
  // single replica (observed 2026-09-20: 15434/0/0). New connections
  // re-resolve through the compose DNS round-robin and spread the load.
  noConnectionReuse: true,
  stages: [
    { duration: '20s', target: 10 },
    { duration: '60s', target: 10 },
    { duration: '10s', target: 0 },
  ],
  thresholds: {
    // p50 is k6's med; intentional 409/422 responses pass their checks and
    // never count as errors, so error_rate covers only 5xx/unexpected.
    http_req_duration: ['p(50)<300', 'p(95)<800', 'p(99)<1500'],
    checks: ['rate>0.99'],
    error_rate: ['rate<0.01'],
  },
};

const processed = new Counter('load_processed');
const replays = new Counter('load_replays');
const conflicts = new Counter('load_conflicts_409');
const rejected = new Counter('load_rejected_422');
const pending = new Counter('load_pending_202');
const errorRate = new Rate('error_rate');
const submitLatency = new Trend('load_submit_latency_ms');

function token(clientId, secret) {
  const res = http.post(
    `${KEYCLOAK}/protocol/openid-connect/token`,
    { grant_type: 'client_credentials', client_id: clientId, client_secret: secret },
    { headers: { 'Content-Type': 'application/x-www-form-urlencoded' } },
  );
  check(res, { 'token issued': (r) => r.status === 200 });
  return res.json('access_token');
}

export function setup() {
  // Readiness: the api has no healthcheck under --scale, so wait for it
  // instead of racing the first wallet creation (setup runs once; a refused
  // connection here fails the whole run downstream with 400s).
  for (let i = 0; i < 30; i++) {
    const live = http.get(`${API}/health/live`);
    if (live.status === 200) break;
    sleep(1);
    if (i === 29) throw new Error('api never became ready');
  }
  const internal = token(INTERNAL_CLIENT, INTERNAL_SECRET);
  const provider = token(PROVIDER_CLIENT, PROVIDER_SECRET);
  const wallets = [];
  for (let i = 0; i < WALLETS; i++) {
    const playerId = uuidv4();
    const res = http.post(
      `${API}/wallets`,
      JSON.stringify({ playerId, currency: 'BRL', initialAmount: FUNDING }),
      { headers: { Authorization: `Bearer ${internal}`, 'Content-Type': 'application/json' } },
    );
    check(res, { 'wallet opened': (r) => r.status === 201 });
    wallets.push({ id: res.json('walletId'), playerId });
  }
  return { provider, internal, wallets, runId: Date.now() };
}

const AMOUNTS = ['1.00', '2.00', '5.00'];

// wallets.player_id is UUID-typed: mint RFC 4122 v4 ids without imports.
function uuidv4() {
  return 'xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx'.replace(/[xy]/g, (c) => {
    const r = (Math.random() * 16) | 0;
    return (c === 'x' ? r : (r & 0x3) | 0x8).toString(16);
  });
}

export default function (data) {
  const w = data.wallets[exec.vu.idInTest % data.wallets.length];
  const roll = Math.random();
  const headers = {
    Authorization: `Bearer ${data.provider}`,
    'Content-Type': 'application/json',
  };

  let key;
  let amount;
  // Keys are run-scoped: replay/conflict lanes reuse keys by design, and a
  // stale key from a previous run (different walletId in the hash) would
  // 409 against this run's wallets instead of replaying.
  if (roll < 0.85) {
    // Fresh operation: unique key, unique external id.
    key = `k6-${data.runId}-${exec.vu.idInTest}-${exec.scenario.iterationInTest}`;
    amount = AMOUNTS[Math.floor(Math.random() * AMOUNTS.length)];
  } else if (roll < 0.95) {
    // Replay: same key + same content as the VU's last fresh submit.
    key = `k6-${data.runId}-replay-${exec.vu.idInTest}`;
    amount = '2.00';
  } else {
    // Conflict: same key, divergent amount.
    key = `k6-${data.runId}-conflict-${exec.vu.idInTest}-${Math.floor(exec.scenario.iterationInTest / 20)}`;
    amount = roll < 0.975 ? '2.00' : '3.00';
  }
  headers['Idempotency-Key'] = key;

  const body = JSON.stringify({
    providerId: PROVIDER_CLIENT,
    externalTransactionId: key,
    playerId: w.playerId,
    walletId: w.id,
    roundId: 'k6-round',
    gameId: 'k6-game',
    kind: 'BET',
    amount,
    currency: 'BRL',
  });

  const res = http.post(`${API}/wagering/transactions`, body, { headers });
  submitLatency.add(res.timings.duration);

  const ok = check(res, {
    'submit accepted': (r) => [200, 201, 202, 409, 422].includes(r.status),
  });
  if (!ok) {
    errorRate.add(1);
    return;
  }
  errorRate.add(0);
  if (res.status === 201) processed.add(1);
  else if (res.status === 200) replays.add(1);
  else if (res.status === 409) conflicts.add(1);
  else if (res.status === 422) rejected.add(1);
  else if (res.status === 202) pending.add(1);
  sleep(0.05);
}

// Post-load proof: every wallet reconciles (stored == replayed ledger)
// after the concurrent bombardment.
export function teardown(data) {
  const headers = { Authorization: `Bearer ${data.internal}` };
  for (const w of data.wallets) {
    const res = http.get(`${API}/wallets/${w.id}/reconciliation`, { headers });
    check(res, {
      'wallet reconciles after load': (r) => r.status === 200 && r.json('consistent') === true,
    });
  }
}
