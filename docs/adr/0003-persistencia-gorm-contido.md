# ADR-0003: Persistência — GORM contido

- Status: aceito
- IDs: D07, D08, D09, D12–D15 · Base: README §§4, 5, 6 · Specs: `wallet-ledger`, `wagering`

## Contexto

O README prefere `pgx` com SQL explícito; GORM é aceito desde que transações,
locks e constraints fiquem explícitos e verificáveis. Exige ainda migrations
versionadas com up/down e ledger append-only garantido no banco.

## Decisão

PostgreSQL 18, GORM só como executor com modelos separados do domínio;
proibidos `AutoMigrate`, `gorm.Model`, soft-delete e `Save` em imutáveis.
Invariantes no schema: `balance >= 0`, `balanceAfter = before ± amount`,
`UNIQUE(walletId, transactionId)`, duplo unique financeiro, discriminador
interno/externo com CHECKs, índice parcial anti-OPENING-duplo, trigger anti
UPDATE/DELETE + papel `app` sem esses privilégios. Migrations goose via job
separado (`up`, `down-to 0`). UUIDv7 na aplicação; `Money` em `BIGINT`+`CHAR(3)`.

## Consequências

- Correção independe de lock local ou dedup do broker (§5.3).
- Alternativa pgx puro preterida pelo custo de prazo; revisão dos SQLs
  gerados (`FOR UPDATE`, `SKIP LOCKED`) nos testes de integração.
