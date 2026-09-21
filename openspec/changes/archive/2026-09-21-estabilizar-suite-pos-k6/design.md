# Design

## Context

Ver `proposal.md` (Why). Estado atual que molda a abordagem:

- `Makefile:31-33` — `test-integration` faz `docker compose stop consumer workers` + `go test -tags integration ./tests/` (sem `-count=1`, sem purge, sem religar ao final).
- `Makefile:35-37` — `k6` escala `api=3` e publica ~23k eventos nas filas locais; o restore (`docker compose up -d`, 1×api) está documentado só em `docs/k6-report.md:34-38`, não no alvo.
- `tests/sqs_workers_test.go:48-89` — `drainEvents` recebe no máx. 10 rodadas × 10 msgs (= 100) e filtra por `walletID`; com backlog do k6 à frente, a wallet nova nunca aparece → `events 0, want 4` (observado 2026-09-21).
- Outbox no Postgres drena sozinha (0 unpublished após k6); o resíduo problemático é nas filas SQS do emulador (`wager-events.fifo`, `wager-transactions.fifo` + 2 DLQs).

## Goals / Non-Goals

**Goals:**

- `make test-integration` verde e determinístico independente do que rodou antes (k6 ou não).
- Falhas futuras de contaminação virarem erro explícito de setup, não `FAIL` misterioso em teste de domínio.

**Non-Goals:**

- Mudar semântica de produção (ordem de eventos, leases, DLQ, retries) — harness apenas.
- Reescrever a suite de integração ou os thresholds do k6.
- Tocar em migrations, Compose de produção ou Keycloak.

## Decisions

1. **Purge das 4 filas locais no início de `test-integration` (SQS `PurgeQueue` contra `http://localhost:4566`), em vez de recriar o stack.**
   Racional: purge é segundos e preserva containers/redes; `down -v` destruiria o Postgres e exigiria migrate+seed do Keycloak (~minutos). Alternativa considerada (deletar via `ReceiveMessage`+`DeleteMessage` em loop) é mais lenta e compete com o `visibility timeout`; `PurgeQueue` é atômica no emulador e foi validada na sessão (4× `PurgeQueueResponse` OK via Query API).
2. **`test-integration` passa `-count=1` e religa `consumer workers` ao final (com `||`/`trap` que religa mesmo em falha).**
   Racional: sem `-count=1` o `go test` retorna `cached` e o veredito é falso; sem religar, o dev sai da suite com topologia degradada (observado: consumer/workers `Stopped` após o alvo). Alternativa (deixar religamento manual) já provou gerar erro humano nesta sessão.
3. **`drainEvents` com teto ampliado + purga prévia como garantia primária (defesa em profundidade), em vez de só uma das duas.**
   Racional: purge resolve a causa (fila limpa no setup); teto maior (p.ex. drenar até 2 rodadas vazias consecutivas, com limite total de ~60s) protege contra resíduos parciais sem tornar o teste lento no caso comum. Alternativa (asserção só em `unpublishedIDs` do Postgres) perderia a cobertura broker real que o teste existe para dar (`TestOutboxBrokerDelivery`).
4. **Documentar a sequência `make k6` → purge → `docker compose up -d` no README § testes (uma linha + referência ao k6-report), em vez de automatizar o restore dentro do alvo `k6`.**
   Racional: o alvo `k6` propositalmente deixa 3×api para inspeção pós-carga (métricas por réplica); restore automático esconderia esse estado. Documentar mantém flexibilidade e fecha a lacuna que causou a contaminação.

## Risks / Trade-offs

- [Purge contra endpoint errado apaga filas] → Mitigação: URL e conta fixas no Makefile/script (`localhost:4566`, conta local), nunca parametrizar contra AWS real; alvo só existe no Compose local.
- [`PurgeQueue` tem janela de ~60s no SQS real onde um segundo purge falha] → Mitigação: irrelevante no emulador; ainda assim, purge uma vez por invocação, não por teste.
- [Ampliar `drainEvents` pode alongar falhas reais] → Mitigação: teto de tempo (~60s) + saída antecipada em 2 rodadas vazias; no caminho feliz o custo extra é ~2× `WaitTimeSeconds`.
- [Religar workers no final do alvo pode mascarar falha da suite se o `up -d` falhar] → Mitigação: preservar o exit code da suite (`status=$? ... exit $status`).
