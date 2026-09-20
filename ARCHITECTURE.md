# ARCHITECTURE.md — Carteira Seamless em Go

Solução do desafio backend (§15): processamento distribuído de apostas com
precisão monetária, ledger auditável e correção sob concorrência e falhas.
Este documento é a síntese narrativa; os porquês vivem nos ADRs
(`docs/adr/`), os contratos testáveis nas specs
(`openspec/changes/consolidar-fundacoes-e-documentacao/specs/`), e a
operação no [README](README.md) e em [`docs/k6-report.md`](docs/k6-report.md).

Convenção: cada seção termina com a trilha
`ADR-xxxx · D-ids · README §seção`.

## 1. Visão geral

Binário único em Go 1.26 com três modos (`api`, `consumer`, `workers`),
PostgreSQL 18 como fonte de verdade, MiniStack como SQS FIFO local e
Keycloak OIDC. Domínio puro no centro; adaptadores nas bordas; composição
por Uber Fx com lifecycle e shutdown gracioso.

```
provider-a/b (Keycloak client_credentials)
      |  POST /wagering/transactions (Idempotency-Key)
      v
+-----------+   SQS wager-transactions.fifo   +----------+
| api :8080 |                                 | consumer |
| gin + Fx  |-- outbox (mesmo commit)         | batch 10 |
+-----+-----+                                 +-----+----+
      | BEGIN; FOR UPDATE wallet; ... ; COMMIT       |
      v                                              v
+------------------+   wager-events.fifo   +------------------+
| PostgreSQL 18    |<-- workers (outbox +  | MiniStack :4566  |
| wallet, tx,      |    referências)       | DLQs + redrive   |
| ledger, inbox,   |                       +------------------+
| outbox, work     |
+------------------+
```

Trilha: `ADR-0002 (D02, D05, D06) · README §§ Ambiente, Modos de operação`.

## 2. Dinheiro (Money)

`int64` em unidades mínimas, parse estrito de `"25.00"` (sem normalização
antes do hash; `LOSS` exige `"0.00"`), overflow tratado em parse/soma/sub/
negação (inclui `MinInt64`), **sem teto de negócio além do `int64`**.
Persiste como `BIGINT` + `CHAR(3)`; BRL principal, USD para
incompatibilidade. Float é proibido em todo o caminho (§5).

Trilha: `ADR-0004 (D10, D11, D21, D23) · README § Testes (unitários)`.

## 3. Transações e máquina de estados

Tipos externos `BET` (débito), `WIN` (crédito, referência opcional), `LOSS`
(sem movimento), `REFUND` (crédito de `BET`), `ROLLBACK` (void de
`BET`/`WIN`/`REFUND`); `OPENING` é interno e rejeitado nas bordas com
`OPENING_NOT_ALLOWED`. Estados: `PENDING → PROCESSED | REJECTED |
PENDING_REFERENCE | FAILED`, `PENDING_REFERENCE → PROCESSED | REJECTED |
FAILED`; terminais nunca transitam. `FAILED` é só falha permanente de
infra (sem evento); `REJECTED` é regra de negócio (persistido + evento).

Trilha: `ADR-0003 (D12–D15) · README § Testes`.

## 4. Idempotência em duas camadas

**Financeira:** chave opaca obrigatória (`Idempotency-Key`, 1–255, espaços
permitidos), SHA-256/RFC8785 de 10 campos de negócio (chave e transporte
fora → mesmo hash via HTTP e SQS). Duplo unique com handler de violação:
`(provider, chave)` ou `(provider, externalId)` igual + hash igual → replay
`200` com o saldo original; hash diferente → `409 IDEMPOTENCY_CONFLICT`.
Corrigíveis (`400`) não persistem nem reservam chave. 50× a mesma aposta em
paralelo (HTTP+SQS misturados) resulta em 1 débito (`tests/concurrency_test.go`).

**Transporte:** inbox `(consumer, messageId)` com verificação de hash;
mesmo `messageId` + conteúdo igual → replay; conteúdo divergente →
envenenamento → DLQ. Correlação: `X-Correlation-Id` ou gerado; SQS deriva
do `messageId`; logs, métricas e spans OTel-stdout compartilham o valor.

Trilha: `ADR-0007 (D18–D20, D39, D45) · README §§ Exemplos, Testes`.

## 5. Concorrência: lock por carteira

`BEGIN; SELECT wallet FOR UPDATE; valida; INSERT tx + ledger + outbox
(+inbox/work); UPDATE version+1; COMMIT` — sem lock global (proibido §5.6).
`version` inicia em 1 e só incrementa em mudança de saldo (é a sequência do
cursor do ledger). Referência e operação concordam em wallet → mesmo lock,
sem deadlock na cascata. Carteiras distintas avançam em paralelo sem lost
updates; 3 instâncias independentes sobre o mesmo banco convergem
(`tests/concurrency_test.go`: 100/80/80 decide exatamente um vencedor).

Trilha: `ADR-0005 (D16, D30) · README § Testes (concorrência)`.

## 6. Referências pendentes e work-table

Referência ausente → `PENDING_REFERENCE` + evento + linha em `work_items`
(`kind`, contadores separados 5 ref / 10 infra, `next_attempt_at`, lease),
nunca descarte silencioso. Aceite híbrido: a mesma tx tenta concluir inline
(`201`/`422`); senão `202` e o worker assume (lease + `SKIP LOCKED`).
Resolução imediata na tx em que a referência processa + polling 500ms;
backoff full-jitter 200ms→3s; **TTL 60s**. Esgotado → `REJECTED
REFERENCE_NOT_FOUND` + evento; referência terminal sem sucesso →
`REFERENCE_NOT_PROCESSED`. Kill após `PENDING` é retomado por outra
instância por construção (§13.8; `tests/recovery_test.go`, `resume_test.go`).

Trilha: `ADR-0006 (D17, D25, D27, D29c) · README § Testes (falhas)`.

## 7. Reversões seamless anti-overpayment

`REFUND` só de `BET` processada; `ROLLBACK` de `BET`/`WIN`/`REFUND` processada;
nunca de `LOSS`/`OPENING`/não-processada, nunca `ROLLBACK` de `ROLLBACK`;
uma reversão direta por `BET` (`ALREADY_REVERSED`); **`BET` com `WIN`
processado dependente não revertido bloqueia (`BET_HAS_ACTIVE_WIN`)** —
stake de volta + prêmio seria criar dinheiro. `ROLLBACK` de `REFUND` debita
(void) e libera a `BET` (cadeia `BET→REFUND→ROLLBACK-do-REFUND`). Valores
integrais, concordância em provedor/jogador/carteira/moeda/rodada; reversão
que quebraria o saldo → `ROLLBACK_INSUFFICIENT_BALANCE`. `WIN` com
referência exige `BET` processada não revertida (mesma rodada/carteira/moeda).

Trilha: `ADR-0008 (D24, D26, D28) · README § Testes (unitários)`.

## 8. Inbox, outbox e eventos

Estado+saldo+ledger+inbox+eventos no mesmo commit; publicação SOMENTE
pós-commit por worker separado com claim `SKIP LOCKED` + lease com
expiração e backoff; republicação preserva `eventId`. Ordem best-effort:
`MessageGroupId = walletId`, dedup = `eventId`, consumidor deduplica por
`eventId` e ordena por `walletVersion` (eventos sem bump carregam a versão
corrente). Eventos: `Processed` (inclui `LOSS`), `Rejected`,
`BalanceChanged` (só em mudança efetiva), `PendingReference`; nada para
`FAILED`. Inválida/permanente → DLQ explícita; `REJECTED` definitivo sai da
fila após commit; transitória retenta via `ChangeMessageVisibility`
(base 1s, teto 30s). Abertura positiva grava carteira + `OPENING` + ledger +
2 eventos na mesma tx.

Trilha: `ADR-0009 (D40a, D42–D44) · README §§ Filas, Testes`.

## 9. Auth e autorização

Keycloak com realm importado, `client_credentials`
(`provider-a`, `provider-b`, `internal-service`, `test-short-lived` de 20s
para o teste de expiração). JWT validado localmente por JWKS (assinatura,
`iss` estrito, `exp`/`nbf` com 30s de leeway); `providerId` vem de claim
mapeada, nunca do corpo sem conferir. Roles: `provider` (só as próprias
transações, inclusive replays — `403 PROVIDER_FORBIDDEN` sem dados fora do
escopo) e `internal` (carteiras, ledger, reconciliação, qualquer leitura).
Contrato: `400` corrigível sem persistência, `422` `REJECTED` persistido com
evento, `500` `FAILED` sem evento, `409` conflito, `202` pendente, `503` +
`Retry-After`. Broker com credenciais por papel (não numéricas) + policies;
**o emulador pode não aplicar** (ver §11) — o consumidor sempre revalida o
domínio. Logs JSON com correlationIds (sem payload financeiro); Prometheus
+ OTel stdout; `/metrics` em porta admin; readiness estrito (PG+SQS).

Trilha: `ADR-0010 (D31–D36, D41, D45–D51, D54, D55) · README §§ Exemplos, Operação`.

## 10. Composição Fx e ciclo de vida

Cada modo é um `fx.Module` (`api`, `consumer`, `workers`); outbox e
referências vivem em `workers` (escalável), nunca só no `api`. Config por
env validada no start (falha rápida); domínio independente de
Fx/HTTP/SQS/GORM. Shutdown: para novas entradas, conclui ou libera o voo
(consumer: 30s ou libera visibility; workers: cancela loops; servidores:
drain com deadline), fecha dependências após os consumidores.
`tests/fx_composition_test.go` sobe/desce cada modo contra infra real, sem
mocks, e prova a liberação religando as mesmas portas.

Trilha: `ADR-0002 (D02, D05, D06) · README § Modos de operação`.

## 11. Limitações conhecidas e resultado do teste do emulador

`tests/sqs_compat_test.go` (tag `integration`) fixa o comportamento
observado do MiniStack `1.5.13`:

- **Policies não aplicadas**: queue policies são armazenadas mas não
  impostas — por isso as validações de domínio no consumidor são
  obrigatórias e as credenciais por papel são defense-in-depth.
- **Handles obsoletos**: release-to-0 com receipt antigo é ignorado —
  os handles do consumidor são sempre correntes; redelivery replays é
  idempotente pela inbox.
- **Credenciais**: chaves não numéricas caem na conta padrão e enxergam
  as mesmas filas (chave de 12 dígitos vira ID de conta e isola);
  documentado como limite, não como garantia.
- FIFO/grupo/dedup/visibility/long-poll/redrive (`maxReceiveCount 5`)
  comportam-se como o esperado nos testes.

Outras limitações: sem lock global por desenho (§5.6) — ordem de eventos é
best-effort com ordenação no consumidor; TTL 60s é parâmetro documentado,
curto para operação manual; sem teto de valor além do `int64` (tema de
produto); `playerId` não-UUID responde `503` em vez de `400` (achado do k6,
ver `docs/k6-report.md` § Findings — correção exige toque de contrato,
fora deste escopo).

Trilha: `ADR-0001 (D37) · README §§ Filas, Solução de problemas`.

## 12. Verificação executada (§13 + diferenciais)

- **Concorrência** (`tests/concurrency_test.go`, `-race`): 50× mesma aposta
  (HTTP+SQS) → 1 débito + 49 replays; 100/80/80 → exatamente um vencedor;
  8 carteiras em paralelo; 3 instâncias independentes; ledger `stored ==
  calculado` em tudo.
- **Recuperação** (`tests/recovery_test.go`, `resume_test.go`,
  `sqs_consumer_test.go`, `sqs_workers_test.go`): kill entre commit e
  delete (round-trip no broker), outbox disputada pós-lease expirado com
  exactly-once por evento, REFUND-antes-da-referência com resolução e com
  expiração, restart preservando idempotência.
- **Composição** (`tests/fx_composition_test.go`): start/stop por modo com
  religamento; `go test ./...`, `-race`, `go vet` (ambas as tags) verdes.
  O teste expôs e o commit corrigiu construtores Fx que recebiam
  `context.Context` (não fornecível pelo Fx).
- **Carga** (`docs/k6-report.md`, `tests/k6/wallet_load.js`, `make k6`):
  3 réplicas api, 102 rps, p50 28.8ms / p95 51.9ms / p99 67.2ms,
  0 erros, 7 conflitos intencionais, 10/10 carteiras reconciliadas;
  atraso de outbox sob worker único ≈139s em rajada (drena sem perda).
- **Suite integração**: ~85s (inclui sleep de ~50s do teste de token
  expirado); exige `docker compose stop consumer workers` antes — os
  workers atuais competem com os publishers dos testes pelo outbox
  compartilhado (`make test-integration` já faz isso).

## 13. Índice de rastreabilidade

| ADR | Decisão | D-ids | Base (README do desafio) | README (solução) |
|-----|---------|-------|--------------------------|------------------|
| 0000 | Usar ADRs | — | §15 | [Modos de operação](README.md#modos-de-operação) |
| 0001 | MiniStack | D37 | §§4, 10, 15 | [Filas](README.md#filas), [Solução de problemas](README.md#solução-de-problemas) |
| 0002 | Modos Fx | D02, D05, D06 | §4 | [Ambiente](README.md#ambiente), [Modos de operação](README.md#modos-de-operação) |
| 0003 | GORM contido | D07–D09, D12–D15 | §§4–6 | [Migrations](README.md#migrations), [Testes](README.md#testes) |
| 0004 | Money int64 | D10, D11, D21, D23 | §6.1 | [Testes](README.md#testes) |
| 0005 | FOR UPDATE | D16, D30 | §§5, 8 | [Testes](README.md#testes) |
| 0006 | Work-table | D17, D25, D27, D29c | §§6.3, 7 | [Testes](README.md#testes) |
| 0007 | Idempotência | D18–D20, D39, D45 | §§5, 9, 10 | [Exemplos](README.md#exemplos-autenticados), [Testes](README.md#testes) |
| 0008 | Reversões | D24, D26, D28 | §7 | [Testes](README.md#testes) |
| 0009 | Outbox | D40a, D42–D44 | §§5, 11 | [Filas](README.md#filas), [Testes](README.md#testes) |
| 0010 | Auth OIDC | D31–D36, D41, D45–D51, D54, D55 | §§2, 9, 12 | [Exemplos](README.md#exemplos-autenticados), [Operação](README.md#operação) |
