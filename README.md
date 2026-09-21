# Carteira Seamless — solução em Go

> Enunciado original do desafio (definição de pronto) em
> [`REQUISITOS.md`](REQUISITOS.md).

Processamento distribuído de apostas com precisão monetária, ledger
auditável e correção sob concorrência e falhas: API HTTP + consumidor SQS
com garantias equivalentes, carteiras com saldo nunca negativo,
idempotência persistente cross-channel e reversões anti-overpayment.

Leia primeiro: [`ARCHITECTURE.md`](ARCHITECTURE.md) (decisões),
[`docs/adr/`](docs/adr/) (log de decisões), [`docs/k6-report.md`](docs/k6-report.md)
(carga reproduzível).

## Pré-requisitos

- **Go 1.26** (`mise install` — `mise.toml` fixa a toolchain).
- **Docker + Docker Compose** (imagens externas fixadas: `postgres:18.6`,
  `ministackorg/ministack:1.5.13`, `keycloak/keycloak:26.7.4`,
  `grafana/k6:2.2.0` só no perfil de carga).
- **mise** (também entrega o `k6 = "2.2.0"` com `mise.lock` commitado).
- Portas livres no host: `5433` (postgres), `8080`/`9090` (api),
  `8081` (keycloak), `4566` (ministack).

## Configuração

```bash
cp .env.example .env   # opcional: os defaults já sobem tudo
```

Variáveis principais (ver `.env.example`): `DATABASE_URL`,
`MIGRATE_DATABASE_URL` (papel `owner`, só no job de migrate),
`SQS_ENDPOINT`, credenciais por papel (`SQS_CONSUMER_KEY/SECRET`,
`SQS_PUBLISHER_KEY/SECRET`), `OIDC_ISSUER`, `APP_MODE`.

## Ambiente

```bash
make up       # build + sobe tudo (postgres, ministack, keycloak, migrate, api, consumer, workers)
make ps       # estado dos serviços
make health   # live/ready da api, postgres, ministack, keycloak
make logs SERVICE=api   # tail dos logs (ou sem SERVICE para todos)
```

## Modos de operação

Um binário, um modo por processo (`APP_MODE`): `api` (HTTP `:8080` +
admin `:9090`), `consumer` (poll SQS batch 10/10 handlers), `workers`
(publicador da outbox + retomada de referências, polling 500ms).
Réplicas independentes por papel; `docker compose up -d --scale api=3`
para a topologia de carga (ver `docker-compose.load.yml` e `make k6`).

## Filas

Provisionadas pelo hook `deployments/ministack/ready.d/01-queues.sh`:

- `wager-transactions.fifo` + DLQ `wager-transactions-dlq.fifo`
  (redrive `maxReceiveCount 5`), `MessageGroupId = walletId`,
  `MessageDeduplicationId = messageId`;
- `wager-events.fifo` + DLQ, `MessageGroupId = walletId`,
  `MessageDeduplicationId = eventId`.

Limites do emulador (policies não aplicadas, handles obsoletos) em
[`ARCHITECTURE.md`](ARCHITECTURE.md#11-limitações-conhecidas-e-resultado-do-teste-do-emulador).

## Migrations

Versionadas com goose (`migrations/`, job `migrate` no compose).
Migrations exigem o papel `owner` (DDL); do host, exporte a URL com a
porta publicada (`5433`) — o `.env` traz o host in-network (`postgres`):

```bash
export DATABASE_URL=postgres://owner:ownersecret@localhost:5433/wallet?sslmode=disable
make migrate-up     # aplica pendentes
make migrate-down   # reverte TUDO (pede confirmação; down-to 0)
```

## Exemplos autenticados

Tokens via Keycloak `client_credentials` (realm `wallet` importado):

```bash
A=$(curl -s -X POST http://localhost:8081/realms/wallet/protocol/openid-connect/token \
  -d grant_type=client_credentials -d client_id=provider-a -d client_secret=provider-a-secret | python3 -c "import json,sys; print(json.load(sys.stdin)['access_token'])")
I=$(curl -s -X POST http://localhost:8081/realms/wallet/protocol/openid-connect/token \
  -d grant_type=client_credentials -d client_id=internal-service -d client_secret=internal-secret | python3 -c "import json,sys; print(json.load(sys.stdin)['access_token'])")

# Carteira (papel internal) -> 201
curl -s -X POST http://localhost:8080/wallets \
  -H "Authorization: Bearer $I" -H 'Content-Type: application/json' \
  -d '{"playerId":"<uuid>","currency":"BRL","initialAmount":"100.00"}'

# Aposta (papel provider, escopo próprio) -> 201; replay -> 200; conflito -> 409
curl -s -X POST http://localhost:8080/wagering/transactions \
  -H "Authorization: Bearer $A" -H 'Content-Type: application/json' \
  -H 'Idempotency-Key: <chave-unica>' \
  -d '{"providerId":"provider-a","externalTransactionId":"<ext>","playerId":"<uuid>","walletId":"<id>","roundId":"r1","gameId":"g1","kind":"BET","amount":"80.00","currency":"BRL"}'

# Ledger + reconciliação (internal)
curl -s -H "Authorization: Bearer $I" http://localhost:8080/wallets/<id>/ledger
curl -s -H "Authorization: Bearer $I" http://localhost:8080/wallets/<id>/reconciliation
```

`provider-b` nas rotas de `provider-a` (ou corpo com outro `providerId`)
responde `403 PROVIDER_FORBIDDEN` sem dados.

## Operação

- `GET /health/live` (processo) e `GET /health/ready` (postgres + SQS,
  estrito) — públicos na porta principal.
- `GET /metrics` (Prometheus) na porta admin `:9090`, sem auth.
- Logs JSON com `correlationId` (`X-Correlation-Id` ou gerado; SQS deriva
  do `messageId`); tracing OTel com exportador stdout
  (`OTEL_TRACES_STDOUT=0` desliga) — spans carregam `correlationId`.

## Testes

```bash
go test ./...                    # unitários (Money, Wallet, máquina de estados, matriz de reversão, hash)
go test -race ./...              # unitários com race detector
go vet ./... && go vet -tags integration ./...   # ambas as tags
make test-integration            # integração: para consumer/workers e roda
                                 # go test -tags integration ./tests/ (~85s)
make k6                          # carga: 3 apis + k6 in-network (docs/k6-report.md)
```

Build tag: `integration` (arquivos em `tests/`, infra real — sem mocks).

| Arquivo | O que prova |
|---|---|
| `atomicity_test.go`, `reconcile_test.go` | commit atômico + `stored == calculado` |
| `idempotency_test.go`, `http_contract_test.go`, `auth_oidc_test.go` | replay/conflito, contrato HTTP, isolamento OIDC, token expirado |
| `concurrency_test.go` | 50× mesma aposta, 100/80/80, carteiras paralelas, 3 instâncias (`-race`) |
| `recovery_test.go`, `resume_test.go`, `sqs_consumer_test.go`, `sqs_workers_test.go` | kill commit/delete, outbox disputada, REFUND-antes-da-ref, restart |
| `fx_composition_test.go` | start/stop por modo, liberação de workers/portas |
| `sqs_compat_test.go`, `sqs_credentials_test.go`, `observability_test.go` | limites do emulador, papéis do broker, métricas/logs/spans |

`make test-integration` para `consumer`/`workers` antes: os workers
publicam o outbox compartilhado e disputariam os publishers dos testes.
O alvo garante os `consumer`/`workers` no ar e drena o outbox do Postgres
antes da suite, depois purga as filas SQS locais, roda com `-count=1` (sem
cache) e religa os serviços ao final. Sequência após carga:

```bash
make k6                          # deixa 3×api + backlog nas filas locais
docker compose up -d             # volta a 1×api (publica portas)
make test-integration            # drena outbox, purga filas, roda suite, religa workers
```

Detalhes em `docs/k6-report.md`.

## Solução de problemas

- **Porta ocupada / réplicas sumindo**: `docker compose run` reconcilia
  dependências em escala 1 — use `up -d --scale` e `docker run` para o k6
  (detalhes em `docs/k6-report.md` § Findings).
- **k6 com 401**: tokens precisam de `iss` in-network (`keycloak:8080`);
  rode o k6 dentro da rede do compose (`make k6`).
- **Suite lenta/falhando no outbox**: o alvo drena o outbox do Postgres
  automaticamente antes da suite (timeout de ~300s se os workers não
  acompanham — escale `workers` ou espere). Ver `docs/k6-report.md`.
- **Destrutivos**: `make clean` (mantém volumes), `make nuke` (destrói
  volumes e imagens locais, pede `NUKE`).
