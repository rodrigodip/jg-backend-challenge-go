# Design

## Context

Repo verde (Go 1.25 em `mise.toml`, `openspec/specs/` vazio, sem codigo): vide proposal Why. Restricoes duras do README: Uber Fx com lifecycle, `pgx`-preferencial com SQL/locks/constraints explicitos (GORM aceito sob condicao), PostgreSQL, SQS FIFO via emulador, Keycloak OIDC, migrations versionadas com up/down, `go test -race`, garantias §5 (sem float, idempotencia persistente, invariantes no banco, outbox pos-commit, ledger append-only, sem lock global, sem lost update). Pre-processamento em `decisoes-desafio-backend.md` blocos 1-8 e a analise de fios (teto, chave, dupla unicidade, cadeias de reversao, work-table/TTL, versao em eventos sem saldo, HTTP 400/422) moldam as escolhas abaixo.

## Goals / Non-Goals

**Goals:**

- Congelar modelo de dados e fluxos transacionais que satisfacam todos os eliminatorios do §14.
- Manter correcao independente de garantias do broker (deduplicacao FIFO, ordenacao, enforcement de policy).
- Permitir 3+ replicas sem memoria compartilhada nem lock global.
- Deixar documentacao exigida (§15) como saida de primeira classe, nao apendice.

**Non-Goals:**

- Partidas dobradas (diferencial recusado), backend dedicado de tracing, tuning de performance alem do k6 exigido.
- Multi-moeda alem de BRL principal + USD para incompatibilidade.
- Reversao parcial (fora do desafio por definicao).

## Decisions

### D1. Hexagonal + Fx por modos (`api`, `consumer`, `workers`)

Dominio puro (Money, Wallet, WagerTransaction, LedgerEntry, erros por `errors.Is/As`, sem Fx/HTTP/SQS/GORM); portas para repositorios, clock, `eventPublisher` (outbox writer) e `workScheduler`; adaptadores `http`, `sqs`, `postgres`, `keycloak`, `ministack`. Binario unico com flag de modo; cada modo e um `fx.Module` com `Provide/Invoke` proprios; outbox worker e reference worker vivem em `workers` (escalavel), nunca embutidos so no `api`, para que 3 replicas de cada papel sejam possiveis no Compose e nos testes. Alternativa (monolito sempre-tudo-ligado) foi descartada porque acoplaria readiness e shutdown de papeis distintos.

### D2. GORM contido + SQL explicito + papeis de banco

GORM apenas como executor com modelos de persistencia separados do dominio; proibido `AutoMigrate`, `gorm.Model`, soft-delete e `Save` em imutaveis. Toda invariante critica vive no schema: `CHECK (balance >= 0)`, `CHECK balanceAfter = balanceBefore ± amount`, `UNIQUE (walletId, transactionId)`, `UNIQUE (providerId, idempotencyKey)`, `UNIQUE (providerId, externalTransactionId)`, discriminador interno/externo com CHECKs de nulidade, indice parcial anti-credito-inicial-duplo, trigger anti UPDATE/DELETE no ledger + role app sem esses privilegios (dono executa migrations via job). `Money` persiste como `BIGINT` minor units + `CHAR(3)`. Alternativa pgx puro foi preterida pelo custo de prazo junior; a condicao acima preserva a verificabilidade exigida.

### D3. Concorrencia por carteira: `SELECT ... FOR UPDATE`, sem global

Caminho feliz: `BEGIN; SELECT wallet FOR UPDATE; valida; INSERT transacao + ledger + outbox (+inbox/work conforme canal); UPDATE wallet version+1; COMMIT`. Versao so incrementa em mudanca de saldo (igual a sequencia do cursor). `LOSS`/rejeicao nao tocam saldo/versao. Deadlock improvavel porque referencia e operacao concordam em wallet (mesmo lock). Alternativas (otimista com retry, update atomico condicional) exigiriam retry em todo `REJECTED` por saldo; pessimismo por linha e o mais simples de provar no teste 100/80/80.

### D4. Aceite hibrido + work-table separada da outbox

Tabelas distintas porque tem donos distintos: `work_items` (retomada de `PENDING`/`PENDING_REFERENCE`: `kind`, `ref_attempts`, `infra_attempts`, `next_attempt_at`, `lease_owner`, `lease_expires_at`) vs `outbox` (publicacao: `eventId`, `type`, `aggregateId=walletId`, payload imutavel, `attempts`, `next_send_at`, lease). HTTP: mesma tx grava aceite + work + tenta inline; concluindo responde `201`/`422`; senao `202` e worker assume (lease + `SKIP LOCKED`). SQS: mesma tx grava inbox + dominio + ledger + outbox + work se pendente; delete da fila so pos-commit. Referencia resolvida dispara reavaliacao imediata na mesma tx (`UPDATE work_items ... WHERE ref=(provider,extId) AND state=PENDING_REFERENCE` + tentativa inline, mesma wallet) mais polling fallback 500ms; backoff full-jitter base 200ms teto 3s; TTL 60s / 5 tentativas ref / 10 infra (contadores separados). Alternativa wake-up via broker foi descartada (README §4 exige banco; wake inter-processo nao atravessa replicas).

### D5. Idempotencia em duas camadas (financeira + transporte)

Camada financeira: hash SHA-256/RFC8785 dos 10 campos de negocio; mesmos bytes HTTP e SQS. Handler de violacao de unicidade: `(P,chave)` igual + hash igual => replay; `(P,chave)` igual + hash diferente => `409`; `(P,extId)` igual + hash igual (chave diferente) => replay cross-channel; diferente => `409`. Camada transporte: inbox `(consumer,messageId)` com hash verificado; divergencia de hash no mesmo `messageId` => DLQ (envenenamento). Corrigiveis nao persistem nem reservam chave. Alternativa "primeira escrita vence sem comparar hash" quebraria o replay legitimo exigido.

### D6. Reversoes seamless anti-overpayment (decisao de dominio)

`BET` carrega `reversed_by_{type,tx}`; `WIN`/`REFUND` idem para seu proprio rollback. Regras: `REFUND` so de `BET`; `ROLLBACK` de `BET`/`WIN`/`REFUND`; nunca de `LOSS`/`OPENING`/nao-processada; nunca `ROLLBACK` de `ROLLBACK`; uma direta por `BET`; `BET` com `WIN` dependente ativo bloqueia (`BET_HAS_ACTIVE_WIN`); `ROLLBACK` de `REFUND` debita (void) e libera a `BET` para nova reversao futura — documentado como cadeia suportada. `WIN` com referencia exige `BET` processada nao-revertida, mesma rodada/jogador/carteira/moeda. Alternativa permissiva (reverter `BET` com `WIN` ativo) foi descartada por criar dinheiro.

### D7. Outbox best-effort + consumidor que ordena

Claim `SELECT ... FOR UPDATE SKIP LOCKED` + lease com expiracao; `MessageGroupId=walletId`, `DeduplicationId=eventId`; republicacao preserva `eventId`; consumidor deduplica por `eventId` e ordena por `walletVersion` (eventos sem bump carregam versao corrente). Abertura positiva grava carteira + `OPENING` + ledger + 2 eventos na mesma tx. Alternativa ordem estrita global exigiria lock global (proibido §5.6).

### D8. Auth OIDC + broker com limites declarados

Keycloak realm importado, `client_credentials`, mappers `provider_id` + roles `provider`/`internal`, JWKS local (30s leeway), client de vida curta para teste de expiracao. Matriz: provider so proprio escopo (`403` sem dados fora dele), internal carteiras/ledger/reconciliacao/qualquer leitura. SQS com usuarios por papel + queue policies em chaves nao-numericas (mesma conta MiniStack); `ARCHITECTURE.md` declara que o emulador pode nao enforcar e que o consumidor revalida dominio. MiniStack com tag pinnada + teste de compatibilidade (FIFO/grupo, dedup, visibility, long-poll, redrive, policies) cujo resultado vai ao `ARCHITECTURE.md`. Alternativa LocalStack descartada por exigir conta/token (quebra checkout limpo §15).

### D9. Observabilidade e lifecycle

`slog` JSON + Prometheus; `/metrics` em porta admin sem auth, health publico na principal; `correlationId` (HTTP `X-Correlation-Id` ou gerado; SQS derivado do `messageId`) propagado a logs, metricas e spans OTel-stdout; sem payload financeiro completo nos logs. Fx valida config no start, workers com contexto cancelavel e deadline de 30s, `SIGTERM` libera visibility em vez de perder trabalho. k6 para carga (p50/p95/p99, erros, conflitos, atraso outbox) contra Compose com 3+ replicas.

## Risks / Trade-offs

- [GORM esconde SQL] → Mitigacao: proibir `Save`/scopes magicos em financeiro, revisar `FOR UPDATE`/`SKIP LOCKED` gerados nos testes de integracao.
- [MiniStack jovem/divergente da AWS] → Mitigacao: tag pinnada, teste de compatibilidade, correcao nunca depende do broker (§5.3).
- [Cascata na tx da referencia alonga lock] → Mitigacao: so mesma wallet (lock ja detido), inline limitado a uma passada; restante via polling.
- [TTL 60s ainda curto para operacao manual] → Mitigacao: documentado como parametro; avaliador automatico usa segundos, humano reenvia.
- [Teto removido permite valor gigante ate `int64`] → Mitigacao: overflow tratado + `NUMERIC`-free `BIGINT`; abuso e problema de produto, nao de correcao.
- [Duplo unique aumenta contenção em replay paralelo] → Mitigacao: caminho de violacao e so leitura + comparacao de hash; teste 50x valida.
- [Best-effort desordena eventos] → Mitigacao: `walletVersion` em todo envelope + dedup por `eventId` no consumidor.

## Migration Plan

Projeto verde: sem migracao. Ordem de entrega: goose `up` (job) antes de qualquer app; `down` documentado e testado em integracao; realm Keycloak e filas via hooks antes do primeiro teste; rollback de deploy = `docker compose down` + `goose down` por versao (documentado no README de solucao).

## Open Questions

- Nenhuma que mude specs ou abordagem. Ajustes finos (intervalo de polling 250 vs 500ms, batch SQS 10 vs 5, nivel de log de `amount` alem de IDs) ficam para o apply com evidencia dos testes de carga.
