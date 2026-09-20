# Proposal

## Why

O desafio exige precisao monetaria, ledger auditavel e correcao financeira com multiplas instancias e falhas, mas o pre-processamento em `decisoes-desafio-backend.md` introduz restricoes extras (teto de 1M por entrada, chave "sem espacos") e deixa brechas criticas em aberto (cadeias REFUND/ROLLBACK com WIN dependente, reserva de chave em erro corrigivel, versao em eventos sem saldo, TTL/work-table). Sem congelar essas decisoes a favor do `README.md` (fonte de verdade) e do padrao seamless-wallet de igaming, a implementacao corre risco direto nos criterios eliminatorios do §14.

## What Changes

- Congela as decisoes de fundacao com prevalencia do `README.md`: remove teto de 1M por valor de entrada (vale qualquer positivo em `int64` com overflow tratado); relaxa chave de idempotencia para opaca nao-vazia `<=255` sem restricao de espacos.
- Define semantica de idempotencia persistente: erros corrigiveis nao reservam chave; `409` so com linha persistida e hash divergente; violacao da unicidade `(providerId, externalTransactionId)` com hash igual vira replay (incluindo cross HTTP x SQS) com saldo original.
- Fecha a matriz de reversoes no padrao seamless: uma reversao direta bem-sucedida por BET; BET com WIN `PROCESSED` nao-revertido dependente nao pode ser revertida (`BET_HAS_ACTIVE_WIN`); ROLLBACK de WIN/REFUND permitido como void; documenta cadeias REFUND->ROLLBACK.
- Define work-table unificada (`kind`, contadores separados ref/infra, `next_attempt_at`, lease) com TTL 60s, max 5 tentativas de referencia, max 10 de infra, backoff full-jitter, reavaliacao imediata na tx da referencia + polling fallback.
- Define versionamento de eventos sem saldo (carregam `walletVersion` corrente sem incrementar), cursor de ledger so sobre entradas com saldo, e reconciliacao de abertura zero (`checkedEntries: 0`).
- Fecha contrato HTTP: `400` corrigivel sem persistencia, `422` definitivo (`REJECTED` persistido com evento), `500` `FAILED` persistido sem evento, `409` conflito, `202` pendente, `503` transitorio com `Retry-After`; completa `failureCodes` ausentes (`IDEMPOTENCY_CONFLICT`, `PROVIDER_FORBIDDEN`, `BET_HAS_ACTIVE_WIN`).
- Fixa mensageria MiniStack + Keycloak `client_credentials` + observabilidade (logs JSON, metricas, health) e o plano de documentacao exigida (`ARCHITECTURE.md`, README de solucao, `.env.example`, Compose, migrations up/down).

## Capabilities

### New Capabilities

- `money`: value object monetario, parsing estrito, aritmetica exata e limites (§6.1).
- `wallet-ledger`: carteira, ledger append-only, paginacao por cursor e reconciliacao (§6.2, §6.4, §9).
- `wagering`: transacoes, maquina de estados, referencias pendentes e matriz de reversoes (§6.3, §7).
- `idempotency-contracts`: chaves, hash canonico, replay/conflito e contrato HTTP de envio e consulta (§5, §9).
- `messaging`: inbox SQS, consumidor, outbox transacional, eventos e DLQ (§6.5, §10, §11).
- `auth-observability`: OIDC/Keycloak, matriz de acesso, credenciais do broker, logs/metricas/health e ciclo de vida Fx (§2, §4, §12).

### Modified Capabilities

(nenhuma — `openspec/specs/` esta vazio; tudo e capacidade nova)

## Impact

- Codigo novo (Go 1.25, Fx, gin, GORM, goose, PostgreSQL 18, MiniStack, Keycloak): dominio, persistencia, HTTP, workers SQS/outbox/referencias, auth e observabilidade.
- APIs externas: `POST /wallets`, `GET /wallets/:id`, ledger paginado, `POST /wagering/transactions`, consultas de transacao, reconciliacao, `/health/*`, `/metrics` em porta administrativa.
- Infra local: Docker Compose (api, consumer, workers, migrate job, Postgres, MiniStack, Keycloak, app de teste k6), filas FIFO + DLQs, realm importado.
- Documentacao: `ARCHITECTURE.md` (todas as decisoes §15), README de solucao, `.env.example`.
