# Tasks

## 1. Fundacao e contratos de banco

- [x] 1.1 Criar layout hexagonal, `go.mod` (Go 1.26, ultima estavel), Dockerfile, `.env.example` e verificar `go build ./...` e `gofmt -l` limpo
- [x] 1.2 Escrever migrations goose (wallets, transactions com discriminador, ledger, inbox, outbox, work_items + triggers anti UPDATE/DELETE) e verificar `goose up`/`down` em Postgres 18 real
- [x] 1.3 Criar Compose (postgres, ministack tag pinnada, keycloak com realm importado, migrate job, api/consumer/workers) e verificar `docker compose up --build` sobe todos saudaveis
- [x] 1.4 Provisionar filas `wager-transactions.fifo`, DLQ com redrive, `wager-events.fifo` via hook e verificar via teste de compatibilidade do emulador (FIFO/grupo, dedup, visibility, long-poll, redrive, policies)
- [x] 1.5 Criar Makefile autodocumentado (`make` lista comandos) com up/ps/logs/health/testes/migrate/stop/clean/nuke, aplicar `restart: unless-stopped` nos servicos longos e verificar help, up saudavel e guardas dos destrutivos

## 2. Dominio (Money, Wallet, Wagering)

- [x] 2.1 Implementar `Money` int64 (parse estrito, zero, soma/sub/negacao com overflow, comparacao, serializacao) e verificar unitarios com `-race` (inclui `MinInt64` e USD x BRL)
- [x] 2.2 Implementar agregado `Wallet` (criacao/reidratacao, debito/credito, versao so em mudanca de saldo) e verificar unitarios de invariantes e `WALLET_PLAYER_MISMATCH`
- [x] 2.3 Implementar `WagerTransaction` + `LedgerEntry` (estados, transicoes, `balanceAfter = before ± amount`, `OPENING` interno) e verificar unitarios de maquina de estados e imutabilidade
- [x] 2.4 Implementar regras dos 5 tipos + matriz de reversao com `BET_HAS_ACTIVE_WIN`/`ALREADY_REVERSED` e verificar unitarios de cada aresta da matriz e politica de zero

## 3. Persistencia e caso de uso transacional

- [x] 3.1 Implementar repositorios GORM contidos (`FOR UPDATE` carteira, `SKIP LOCKED` outbox/work, sem `Save`/imuteis) e verificar teste de integracao de atomicidade (estado+saldo+ledger+inbox+outbox no mesmo commit)
- [x] 3.2 Implementar hash SHA-256/RFC8785 + handler de dupla unicidade (replay vs `409`) e verificar integracao HTTP x SQS com mesmo hash e saldo original em replay
- [x] 3.3 Implementar aceite hibrido + work-table (inline ou `202`, lease, backoff, TTL 60s/5 ref/10 infra, reavaliacao imediata + polling) e verificar retomada por outra instancia apos `PENDING`
- [x] 3.4 Implementar cursor de ledger, reconciliacao `REPEATABLE READ` e metricas/log de divergencia e verificar `stored == calculado` e `checkedEntries` incluindo abertura zero

## 4. HTTP + Auth

- [ ] 4.1 Implementar gin + Fx modules (`api`), endpoints wallets/ledger/transactions/reconciliacao/health com codigos `201/200/202/400/422/409/404/503` e verificar contrato por teste de integracao autenticado
- [ ] 4.2 Integrar Keycloak `client_credentials` (JWKS, claims, roles, `403` sem dados, token expirado) e verificar isolamento provider-a x provider-b em consultas e replays
- [ ] 4.3 Implementar `/metrics` em porta admin + logs JSON com correlationIds e verificar presenca de metricas (status, duplicatas, retries, DLQ, conflitos, atraso outbox, latencia, divergencias)

## 5. Mensageria e workers

- [ ] 5.1 Implementar consumidor SQS (batch 10, 10 handlers, visibility 30s, `ChangeMessageVisibility` backoff, DLQ explicita, delete so pos-commit, `SIGTERM` 30s) e verificar reentrega pos-commit sem duplicata
- [ ] 5.2 Implementar workers de referencias e outbox (claim concorrente, lease, `eventId` estavel, envelope com `walletVersion`) e verificar 2 publishers disputando + recuperacao pos-queda
- [ ] 5.3 Implementar credenciais por papel do broker + documentar limite de enforcement e verificar consumidor revalida dominio mesmo com policy permissiva

## 6. Verificacao obrigatoria (§13) e diferenciais

- [ ] 6.1 Executar bateria de concorrencia: 50x mesma aposta, 100/80/80, carteiras distintas em paralelo, 3 instancias independentes, e verificar saldo final + ledger + `go test -race`
- [ ] 6.2 Executar bateria de recuperacao: kill entre commit/remocao, outbox disputada, REFUND antes da referencia com resolucao e com expiracao, restart preservando idempotencia, e verificar consistencia `stored == creditos - debitos`
- [ ] 6.3 Executar teste de composicao Fx (start/stop, liberacao de workers) sem mocks integrais e verificar `go test ./...`, `go test -race ./...`, `go vet ./...` verdes
- [ ] 6.4 Adicionar OTel stdout + cenario k6 (throughput, p50/p95/p99, erros, conflitos, atraso outbox) e verificar relatorio reproduzivel documentado

## 7. Documentacao de entrega (§15)

- [ ] 7.1 Escrever `ARCHITECTURE.md` (dinheiro, transacoes, idempotencia, locks, refs, reversoes, inbox/outbox, auth/authz, Fx, shutdown + limitacoes/resultados do teste de emulador) e verificar cada ADR em `docs/adr/` rastreado ao seu D-id e secao do README
- [ ] 7.2 Escrever README de solucao (pre-reqs, env, filas, migrate up/down, exemplos autenticados, testes unitarios/integracao/multi-instancia/falhas, build tags) e verificar reproducao a partir de checkout limpo
