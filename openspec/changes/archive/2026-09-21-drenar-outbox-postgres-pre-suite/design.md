# Design

## Context

Ver `proposal.md` (Why). Estado que molda a abordagem:

- `Makefile:31-34` — o alvo atual para consumer/workers, purga SQS, roda a suite e religa. O purge SQS resolve o backlog do broker, mas o outbox do Postgres (origem do `Error 1`) fica intacto.
- `internal/adapters/postgres/aux_repo.go:75-104` — `ClaimOutbox` é FIFO (`Order("next_send_at ASC")`, `Limit(limit)`): o backlog do k6 é sempre consumido antes das linhas frescas dos testes.
- `tests/sqs_workers_test.go:108-119` — `publishUntilDrained` faz no máx. 20 rounds × 100 = 2000 e exige quiescência **global**; com ~20k de backlog é falha certa.
- `docs/k6-report.md:70-78` — o backlog drena sem perda; a alavanca é o tempo de workers (publisher único ~85-100 ev/s).

## Goals / Non-Goals

**Goals:**

- `make test-integration` determinístico imediatamente após `make k6`, sem depender de quanto tempo passou para os workers drenarem.
- Setup problemático (outbox que não drena) → erro explícito e acionável, não `FAIL` no meio da suite.

**Non-Goals:**

- Reworkar `TestOutboxPublishDispute` para convergência wallet-scoped (ver Decisões: inviável por causa do FIFO).
- Mexer em semântica de produção (dreno, leases, backoff, DLQ).
- Mudar o cenário do k6 ou os thresholds.

## Decisions

1. **Dreno prévio do outbox no `test-integration`, liderado pelos workers, antes de pará-los.**
   Sequência do alvo: (a) `docker compose up -d consumer workers` (garante drenadores), (b) poll `count(*) FROM outbox WHERE published_at IS NULL` até 0, com teto de ~300s (60 iterações × 5s) e mensagem de erro clara no timeout, (c) `docker compose stop consumer workers`, (d) purge SQS (já existe), (e) `go test -tags integration -count=1`, (f) religa consumer/workers preservando exit code.
   Racional: ataca a causa exata (`aux_repo.go:83` FIFO + `publishUntilDrained` global). Estado limpo = 0 iterações de espera; pós-k6 = espera proporcional ao backlog (~200s no pior caso, já reduzida pelos 90s de dreno durante o próprio k6). Alternativas: `docker compose down -v` (destrói dados, lento) — rejeitada; espera fixa por sleep (não-determinística) — rejeitada.
2. **Não reworkar `TestOutboxPublishDispute` para wallet-scoped.**
   Racional: mesmo wallet-scoped, o `ClaimOutbox` FIFO coloca as linhas do teste no fim do backlog; com limite 100 e deadline de 30s, convergir atravessando ~20k é inviável. O dreno prévio no Makefile é a garantia estrutural; o teste permanece, agora com outbox garantidamente limpo. Rework seria esforço alto com ganho marginal dado o dreno.
3. **Poll via `docker compose exec -T postgres psql` (read-only, local).**
   Comando: `psql -U postgres -d wallet -tAc "SELECT count(*) FROM outbox WHERE published_at IS NULL"`. Racional: sem cliente novo (Postgres já exposto via compose), sem acesso externo. Trimming com `tr -d '[:space:]'` para robustez de formatação.
4. **Documentação: atualizar o comentário do alvo e o README § testes.**
   README já documenta a sequência pós-carga; acrescentar que o alvo também drena o outbox do Postgres (fecha o gap que o `Error 1` revelou).

## Risks / Trade-offs

- [Poll pode segurar a suite por minutos pós-k6] → Mitigação: teto 300s + progresso por iteração; estado limpo sai na 1ª iteração.
- [Outbox que nunca drena segura 5 min e falha] → Mitigação: erro explícito "outbox not drained; workers not keeping up — raise workers or wait", em vez de `FAIL` misterioso.
- [`psql` indisponível/instável no loop] → Mitigação: iteração vazia só repete; timeout final com mensagem de setup.
- [Custo de `docker compose up -d` no início do alvo] → Mitigação: idempotente; no caminho comum já está no ar (no-op rápido).