# Proposal

## Why

A suite de integração voltou a falhar após o k6 (`make: *** Error 1`), mesmo depois do `estabilizar-suite-pos-k6`. A causa: o purge do alvo limpa só as filas **SQS**, não o **outbox no Postgres** — e o k6 deixa ~20k linhas não publicadas. `TestOutboxPublishDispute` exige quiescência global do outbox (`publishUntilDrained`, teto 20×100), que é impossível com backlog de milhares → `errOutboxNeverQuiescent` → `t.Fatal`. Runs subsequentes passaram apenas porque os workers religados drenaram o Postgres no intervalo. Harness ainda meio-determinístico.

## What Changes

- `make test-integration` passa a drenar o outbox do Postgres **antes** de parar os workers: garante `count(outbox WHERE published_at IS NULL) == 0` com poll bounded (workers drenando), só então para consumer/workers, purga SQS e roda a suite.
- Falha de setup vira erro explícito (timeout com mensagem clara), não `FAIL` misterioso no meio da suite.
- Ajuste do comentário do alvo e do README § testes (o alvo agora também drena o outbox).
- Nenhuma mudança em produção: API, domínio, ledger, idempotência, inbox/outbox, auth e migrations intactos.

## Capabilities

### New Capabilities

(nenhuma — mudança de harness/tooling, sem comportamento observável novo)

### Modified Capabilities

(nenhuma — nenhum REQUIREMENT existente muda. A change opta por `skip_specs: true`.)

## Impact

- Afetados: `Makefile` (alvo `test-integration`), `README.md` § testes.
- Não afetados: código de produção (`internal/`, `cmd/`), migrations, contratos HTTP/SQS, Keycloak, Compose de produção.
- Risco: baixo; o poll de dreno é read-only sobre o Postgres local (`docker compose exec psql`), nunca contra ambiente real.