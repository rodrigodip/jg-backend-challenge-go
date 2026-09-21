# Proposal

## Why

A sessão de testes de 2026-09-21 mostrou que a suite de integração é verde no estado limpo (`ok ~83s`) mas falha quando rodada fora do procedimento: com consumer/workers ativos ou com backlog do k6 nas filas locais, `TestOutboxBrokerDelivery` falha (`events 0, want 4`) porque o helper de drenagem tem teto de 100 mensagens e a wallet nova fica enterrada atrás de ~23k eventos do load. Isso torna o veredito "PASS/FAIL" dependente da ordem de execução, não do código.

## What Changes

- Tornar `make test-integration` determinístico: parar consumer/workers, purgar as 4 filas SQS locais (`wager-events`, `wager-transactions` + DLQs) antes da suite, rodar `go test -tags integration -count=1`, e religar consumer/workers ao final.
- Ajustar o helper de drenagem dos testes (`drainEvents`) para não depender de posição na fila: aumentar o teto de polling ou drenar até esvaziar/purge prévio, mantendo as asserções escopadas por wallet.
- Documentar a sequência obrigatória k6 → integração (purge + `docker compose up -d` para voltar a 1×api) no README/docs.
- Nenhuma mudança em comportamento de produção: sem alteração de API, domínio, saldo, ledger, idempotência, inbox/outbox, auth ou migrations.

## Capabilities

### New Capabilities

(nenhuma — mudança de harness/tooling, sem comportamento observável novo)

### Modified Capabilities

(nenhuma — nenhum REQUIREMENT existente muda; specs de `messaging` e `auth-observability` continuam válidos como estão. A change opta por `skip_specs: true` em `.openspec.yaml`.)

## Impact

- Afetados: `Makefile` (alvos `test-integration`, `k6`), helper `tests/sqs_workers_test.go` (`drainEvents`), docs de teste (`README.md` § testes, `docs/k6-report.md` sequência).
- Não afetados: código de produção (`internal/`, `cmd/`), migrations, contratos HTTP/SQS, Keycloak, Compose de produção.
- Risco: baixo; se o purge for aplicado contra endpoint errado apagaria filas — mitigado por fixar `http://localhost:4566` + conta local e rodar só nos alvos de teste.
