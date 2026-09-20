# Registro de decisões — Desafio Backend (Go + Fx)

**Fonte de verdade:** `README.md` do desafio. Nenhuma decisão deste documento pode contradizê-lo.
**Situação:** Blocos 1 a 8 respondidos. **Nenhuma decisão em aberto.**
**Legenda:** ✅ fechada · 🔧 ajustada para cumprir o README · ❓ em aberto

---

## 1. Emulador SQS (D37): verificação e decisão

**Decisão: MiniStack.** LocalStack foi descartado.

| Critério | LocalStack | MiniStack |
| --- | --- | --- |
| Acesso | Desde a versão 2026.03.0 exige conta e auth token para iniciar. O plano gratuito (Hobby) existe, mas só para uso não comercial | MIT; sem conta, chave de API ou telemetria |
| Checkout limpo (README §15: "sem segredos reais") | ❌ Cada avaliador precisaria criar conta e obter um token | ✅ `docker compose up --build` sem credenciais externas |
| SQS FIFO com deduplicação | Não avaliado; o critério de acesso já elimina | ✅ Documentado |
| DLQ | Não avaliado | ✅ "DLQ support" documentado; validar `maxReceiveCount` por teste |
| `ChangeMessageVisibility`, batches, long polling | Não avaliado | ✅ Operações listadas |
| Queue policy | Não avaliado | Operações `SetQueueAttributes`/`AddPermission` existem; **aplicação (enforcement) não confirmada** |
| Provisionamento | — | ✅ Hooks de init (`boot.d`/`ready.d`), endpoint de reset e de health |

**Riscos e cuidados com o MiniStack:**

1. **Projeto jovem, releases frequentes:** fixar a tag exata da imagem no Compose, nunca `latest`.
2. **Multi-tenancy por chave:** uma chave de acesso de 12 dígitos vira ID de conta e isola filas entre contas. Por isso, as credenciais por papel (D51) devem usar chaves **não numéricas**, para que todas caiam na mesma conta padrão e enxerguem as mesmas filas.
3. **Enforcement de políticas não confirmado:** D51 mantém credenciais por papel + queue policies, e o `ARCHITECTURE.md` documenta que o emulador pode não aplicá-las. As validações de domínio no consumidor continuam obrigatórias (README §2).
4. **Semântica não verificada na documentação:** ordenação por `MessageGroupId`, janela de deduplicação de 5 minutos e redrive por `maxReceiveCount`. A correção financeira **não depende** dessas garantias do broker (README §5.3).

**Primeira tarefa de implementação:** um teste de compatibilidade do emulador (`integration`) que valide FIFO por grupo, deduplicação, `ChangeMessageVisibility`, long polling, redrive para a DLQ e o comportamento das queue policies. O resultado entra no `ARCHITECTURE.md`.

---

## 2. Decisões fechadas (Blocos 1 a 4)

### Bloco 1 — Fundamentos

| ID | Decisão |
| --- | --- |
| D01 ✅ | Go na última versão estável, declarada em `go.mod` e no Dockerfile |
| D02 ✅ | Binário único com modos `api`, `consumer` e `workers`, todos escaláveis em réplicas; o worker de outbox roda separado do modo `api` |
| D03 ✅ | gin |
| D04 ✅ | `slog` JSON + Prometheus |
| D05 ✅ | Configuração por variáveis de ambiente, parsing próprio, validada na inicialização (Fx Lifecycle) |
| D06 ✅ | Layout hexagonal; domínio sem Fx, HTTP, SQS ou GORM |

### Bloco 2 — Banco e Money

| ID | Decisão |
| --- | --- |
| D07 ✅ | PostgreSQL 18 |
| D08 ✅ | GORM com condições: transações, locks (`FOR UPDATE`) e constraints explícitos; modelos de persistência separados do domínio; sem `AutoMigrate`, `gorm.Model` nem soft delete; sem `Save` em entidades imutáveis; mapeamento de `Money` documentado |
| D09 ✅ | goose, com `up` e `down` documentados; executado por **job separado** no Compose (D09b) |
| D10 ✅ | `int64` em unidades mínimas + `BIGINT`; overflow tratado em parsing, soma, subtração e negação |
| D11 ✅ | Teto de 1.000.000,00 na moeda (100.000.000 unidades mínimas) por valor de **entrada** (operação e saldo inicial); o saldo não tem teto além do `int64` |
| D12 ✅ | UUIDv7 gerado na aplicação |
| D13 ✅ | Ledger imutável por papel de aplicação sem UPDATE/DELETE **e** trigger; dono das tabelas separado do papel da aplicação |
| D14 ✅ | Unicidade, `balance >= 0`, `(walletId, transactionId)` único e CHECK `balanceAfter = balanceBefore ± amount` |
| D15 ✅ | Tabela única de transações com discriminador interno/externo, CHECKs e índice parcial contra crédito inicial duplicado |

### Bloco 3 — Concorrência e idempotência

| ID | Decisão |
| --- | --- |
| D16 ✅ | `SELECT … FOR UPDATE` na linha da carteira; sem lock global |
| D17 ✅ | Aceite assíncrono com **tabela de trabalho separada** (lease, tentativas, `next_attempt_at`), gravada na mesma transação do aceite; distinta da outbox; serve também ao worker de referências |
| D17b ✅ | **Aceite híbrido:** aceita e enfileira no mesmo commit, tenta processar inline; concluindo, responde o resultado; senão `202` e o worker assume |
| D18 ✅ | Chave única por `(providerId, chave)`; opaca, obrigatória, não vazia, até 255 caracteres, sem espaços |
| D19 ✅ | Chave igual + conteúdo equivalente → replay (`idempotentReplay: true`). Chave igual + conteúdo diferente → 409. `(providerId, externalTransactionId)` igual com chave diferente: equivalente → replay do resultado persistido; diferente → 409 |
| D20 ✅ | SHA-256 sobre JSON canônico (RFC 8785). Campos: `providerId`, `externalTransactionId`, `playerId`, `walletId`, `roundId`, `gameId`, `kind`, `money.amount`, `money.currency`, `referenceExternalTransactionId` (omitido se ausente). Fora do hash: chave e metadados de transporte. HTTP e SQS geram o mesmo hash |
| D21 ✅ | `amount` estrito: só dígitos, um ponto e exatamente duas casas; sem sinal nem espaços; sem normalização antes do hash; `LOSS` exige literalmente `"0.00"` |
| D22 ✅ | `POST /wallets` sem header de idempotência; repetição de `(playerId, currency)` → conflito |

### Bloco 4 — Regras de negócio

| ID | Decisão |
| --- | --- |
| D23 ✅ | Domínio reconhece BRL e USD, ambos com duas casas; cenários principais em BRL; USD sustenta os testes de incompatibilidade |
| D24 ✅ | Matriz de reversões abaixo |
| D24b ✅ | Uma reversão bem-sucedida por `BET`, de qualquer tipo |
| D25 ✅ | Referência ausente → `PENDING_REFERENCE`, worker com backoff, sobrevive a reinício; esgotado → `REJECTED` `REFERENCE_NOT_FOUND` com evento |
| D25b ✅ | Referência existente ainda pendente → aguardar. Referência que terminou `REJECTED`/`FAILED` → `REJECTED` (`REFERENCE_NOT_PROCESSED`) |
| D26 ✅ | `WIN` com referência opcional; se informada, exige `BET` `PROCESSED`, não revertida, da mesma rodada, jogador, carteira e moeda; se ainda indisponível, segue `PENDING_REFERENCE` |
| D27 ✅ | TTL de 15 s; máximo de 5 tentativas; backoff exponencial com full jitter (base 200 ms, teto 3 s); vale o que ocorrer primeiro. Reavaliação imediata no banco (mesma transação em que a referência vira `PROCESSED`) + polling de fallback. A espera acumulada máxima com 5 tentativas é de aproximadamente 6–9 s |
| D28 ✅ | Taxonomia na seção 5 |
| D29 ✅ | `FAILED` só para falha permanente de infraestrutura |
| D29b ✅ | Nenhum evento para `FAILED` |
| D29c ✅ | Falhas transitórias: até N = 10 tentativas com backoff limitado; esgotado → `FAILED` (contador separado das 5 tentativas de `PENDING_REFERENCE`). Permanentes (violação de invariante inesperada, dado persistido inconsistente) vão direto a `FAILED` |
| D30 ✅ | Carteira ↔ `playerId` validado em toda operação; sem vínculo provedor ↔ carteira |

**Matriz de reversões (D24):**

| Alvo | `REFUND` | `ROLLBACK` |
| --- | --- | --- |
| `BET` processada | permitido | permitido |
| `WIN` processada | negado | permitido |
| `REFUND` processada | negado | permitido |
| `ROLLBACK` processada | negado | negado |
| `LOSS`, `OPENING`, transação não processada | negado | negado |

---

## 3. Decisões fechadas (Blocos 5 a 8)

### Bloco 5 — Contrato HTTP

| ID | Decisão |
| --- | --- |
| D31 🔧 | `422` rejeição de negócio; `200` replay; `202` pendente; `400` entrada inválida; `409` conflito de idempotência; `503` com `Retry-After` |
| D31a ✅ | `201` no primeiro processamento concluído (`PROCESSED`); `200` no replay |
| D32 ✅ | Formato de erro próprio, com `failureCode` |
| D33 ✅ | `403` para acesso a transação de outro provedor, sem devolver dados; `providerId` do corpo diferente da identidade → `403` |
| D34 ✅ | Resposta `REJECTED` inclui o saldo observado; `LOSS` devolve o saldo observado; `PENDING` omite o saldo |
| D35 ✅ | Cursor opaco sobre sequência por carteira = versão da carteira após a mudança de saldo, com unicidade `(walletId, sequência)` |
| D36 🔧 | Reconciliação em transação `REPEATABLE READ` somente leitura; reporta divergência em resposta, log e métrica; acesso interno por papel OIDC |

### Bloco 6 — Mensageria e outbox

| ID | Decisão |
| --- | --- |
| D37 ✅ | **MiniStack** (seção 1), imagem com tag fixada |
| D38 ✅ | `MessageGroupId` = `walletId` |
| D39 ✅ | `MessageDeduplicationId` = `messageId` do envelope. Os testes de duplicidade usam IDs de deduplicação distintos para provar a deduplicação da aplicação (§13) |
| D40a ✅ | Mensagem inválida ou erro permanente: envio explícito à DLQ (com `MessageGroupId`, `MessageDeduplicationId` e metadados do erro) e remoção da original |
| D41 🔧 | Acesso controlado por credenciais e políticas do broker; validações de domínio no consumidor (README §2) |
| D42 ✅ | Filas `wager-transactions.fifo` e `wager-transactions-dlq.fifo` (com redrive) provisionadas por hook de init do MiniStack (`ready.d`) |
| D43 ✅ | Eventos de saída em uma fila FIFO `wager-events.fifo`, com DLQ |
| D43b ✅ | `aggregateId` = `walletId` em todos os eventos; `MessageGroupId` = `walletId`; `MessageDeduplicationId` = `eventId` |
| D44 ✅ | Claim da outbox com `SKIP LOCKED` + lease com expiração; ordem best effort. O consumidor deduplica por `eventId` e ordena por `walletVersion` |
| D45 ✅ | HTTP: header `X-Correlation-Id` se presente, senão gerado. SQS: derivado do `messageId` |

### Bloco 7 — Autenticação e autorização

| ID | Decisão |
| --- | --- |
| D46 ✅ | Keycloak, realm importado automaticamente, `client_credentials`; identidades `provider-a`, `provider-b`, `internal-service` e um client de teste com token de vida curta |
| D47 ✅ | JWT validado localmente via JWKS: assinatura, `iss`, `aud`, `exp`, `nbf`, tolerância de relógio de 30 s |
| D48 ✅ | `providerId` vem de claim customizado (protocol mapper no client) |
| D49 ✅ | Client roles `provider` e `internal` |
| D50 ✅ | Token expirado testado com client de vida curta no realm importado |
| D51 ✅ | Credenciais separadas por papel (produtor, consumidor, publisher) + queue policies. **Chaves não numéricas** (mesma conta no MiniStack); limite de enforcement documentado |

**Matriz de acesso:** provedor (`POST /wagering/transactions` e consultas das **suas** transações); serviço interno (carteiras, ledger, reconciliação e leitura de qualquer transação, inclusive `OPENING`). `providerId` do corpo ou do caminho diferente da identidade → `403` sem efeito financeiro.

### Bloco 8 — Testes, observabilidade e entrega

| ID | Decisão |
| --- | --- |
| D52 ✅ | testcontainers-go para PostgreSQL, Keycloak e MiniStack |
| D57 ✅ | Três instâncias independentes nos testes: três containers da mesma imagem |
| D53 ✅ | Build tag `integration` para testes com containers; `go test ./...` roda só unitários; `go test -race` em ambos (imagem de teste com CGO); teste de composição Fx com início e encerramento |
| D54 🔧 | Readiness estrito de PostgreSQL e SQS (README §9); a desvantagem vai para o `ARCHITECTURE.md` |
| D55 ✅ | `/metrics` em porta administrativa separada, sem autenticação; health checks públicos na porta principal |
| D56 ✅ | Diferenciais: **OpenTelemetry** e **testes de carga** (partidas dobradas não entram) |
| D56a ✅ | Tracing: SDK do OpenTelemetry com exportador para **stdout/log**, sem backend adicional (opção mais leve, por ser diferencial opcional). Trocar para OTLP no futuro é só configuração |
| D56b ✅ | Teste de carga com **k6**: comando reproduzível, ambiente, metodologia, throughput, p50/p95/p99, erros, conflitos e atraso da outbox, conforme README §14 |

**Consumidor SQS (K7):** batch de 10; até 10 handlers concorrentes por instância; visibility timeout 30 s; `maxReceiveCount` 5; retry transitório por `ChangeMessageVisibility` com backoff exponencial e jitter (base 1 s, teto 30 s); prazo de shutdown 30 s, findo o qual a visibilidade é liberada. Entrada corrigível, mensagem malformada e conflito de hash com o mesmo `messageId` vão para a DLQ; `REJECTED` definitivo remove a mensagem após o commit.

**HTTP (K11):** `401` credencial ausente, inválida ou expirada; `403` acesso indevido; `400` entradas corrigíveis estruturais; `422` corrigíveis semânticas (`WALLET_*`, `CURRENCY_MISMATCH`) e resultados definitivos; `404` consulta a recurso inexistente; `409` conflito de idempotência ou carteira duplicada; `202` pendente; `503` com `Retry-After`. Corpo de rejeição persistida: `transactionId`, `status: REJECTED`, `failureCode`, `category`, `idempotentReplay`, `balance`. Corpo de erro sem persistência: `failureCode`, `category` (`CORRECTABLE`, `DEFINITIVE`, `TRANSIENT`), `message`, `correlationId`.

---

## 4. Ajustes feitos por conformidade com o README

| ID | Resposta original | Ajuste | Base |
| --- | --- | --- | --- |
| D25 | Rejeitar reversão sem referência | `PENDING_REFERENCE` + worker; `REJECTED` só após TTL/tentativas | §7, §11, §13.7 |
| D26 | `WIN` exige referência | Referência opcional; se informada, valida e pode aguardar | §7 |
| D27 | Wake-up por Kafka/RabbitMQ | Wake-up no banco, na mesma transação | §4 |
| D28, D29 | `FAILED` para regra de negócio | Recusa de negócio é `REJECTED`; `FAILED` é infraestrutura | §6.3, §13.2 |
| D28 | Erros de referência "sem registro" | Persistidos como `REJECTED` com evento | §5.2, §6.3, §7, §11 |
| D28 | Saldo negativo "se permitido" | Nunca | §5, §6.2, §7, §14 |
| D28 | `LIMIT_EXCEEDED`, `ACCOUNT_BLOCKED` | Removidos (não previstos) | — |
| D24 | `ROLLBACK` de `REFUND` omitido | Permitido | §7 |
| D29 | Script manual DLQ → `FAILED` | Removido | §6.3, §10 |
| D30 | Vínculo provedor ↔ carteira | Inexistente | §2, §6.2 |
| D31 | `409` para `ALREADY_REVERSED`, `SUCCESS`, saldo numérico | `409` só para conflito; `PROCESSED`; saldo `{amount,currency}` em string; `idempotentReplay` | §5.1, §6.1, §9 |
| D36 | Rota `/v1/reconciliation`, porta 8081, Istio/K8s | Rota do README; restrição por papel OIDC | §2, §9 |
| D18 | Formato de chave imposto | Chave opaca | §9 |
| D22 | Header em `POST /wallets` | Sem header; duplicidade → conflito | §9 |
| D11 | Teto sobre o saldo | Só sobre valores de entrada | §6.1, §7 |
| D54 | Readiness degradado sem SQS | Readiness estrito | §9 |
| D41 | Mensagem assinada | Credenciais e políticas do broker + validações de domínio | §2 |
| D37 | LocalStack (opção A) | MiniStack, por exigir conta e token de terceiros, o que quebra a reprodução a partir de checkout limpo | §15 |

---

## 5. Taxonomia de `failureCode`

**Entradas corrigíveis:** nada é persistido; o provedor corrige e reenvia.

| Código | Caso |
| --- | --- |
| `INVALID_REQUEST` | JSON malformado, campo ausente ou de tipo errado, `Idempotency-Key` ausente, `kind` desconhecido |
| `INVALID_MONEY` | Formato, escala, sinal, notação, vazio, overflow ou teto de entrada excedido |
| `INVALID_AMOUNT_FOR_KIND` | Zero em `BET`, `WIN`, `REFUND`, `ROLLBACK`; diferente de `"0.00"` em `LOSS` |
| `REFERENCE_REQUIRED` | `REFUND` ou `ROLLBACK` sem `referenceExternalTransactionId` |
| `OPENING_NOT_ALLOWED` | `kind` = `OPENING` por HTTP ou SQS |
| `WALLET_NOT_FOUND` | Carteira inexistente |
| `WALLET_PLAYER_MISMATCH` | `playerId` não é o da carteira |
| `CURRENCY_MISMATCH` | Moeda diferente da da carteira |

**Resultados definitivos:** `REJECTED` persistido, com evento `WagerTransactionRejected`.

| Código | Caso |
| --- | --- |
| `INSUFFICIENT_BALANCE` | `BET` sem saldo |
| `ROLLBACK_INSUFFICIENT_BALANCE` | Reversão que precisaria debitar mais que o saldo |
| `REFERENCE_NOT_FOUND` | Referência não chegou dentro do TTL ou das tentativas |
| `REFERENCE_NOT_PROCESSED` | Referência terminou `REJECTED` ou `FAILED` |
| `INVALID_REFERENCE_KIND` | Alvo não permitido pela matriz, ou `WIN` referenciando algo que não é `BET` |
| `REFERENCE_PLAYER_MISMATCH`, `REFERENCE_WALLET_MISMATCH`, `REFERENCE_CURRENCY_MISMATCH`, `REFERENCE_ROUND_MISMATCH` | Divergência entre operação e referência |
| `AMOUNT_MISMATCH` | Valor da reversão diferente do referenciado |
| `ALREADY_REVERSED` | `BET` já revertida com sucesso, ou alvo já revertido pelo mesmo tipo |
| `REFERENCE_REVERSED` | `WIN` cuja `BET` de referência já foi revertida |

**Falha permanente (`FAILED`):** `INTERNAL_PERMANENT_FAILURE`, sem evento de integração.

**Máquina de estados:** `PENDING` → `PROCESSED`, `REJECTED`, `PENDING_REFERENCE` ou `FAILED`; `PENDING_REFERENCE` → `PROCESSED`, `REJECTED` ou `FAILED`; estados terminais não transitam.

---

## 6. Decisões em aberto

Nenhuma. O levantamento das decisões da stack está completo.

---

## 7. Impactos de design

- **Duas estruturas distintas:** tabela de trabalho (`PENDING` e `PENDING_REFERENCE`, lease, backoff) e outbox de eventos (publicação, lease, backoff).
- **Modo `workers`:** worker de trabalho pendente/referências e worker de outbox, ambos com claim concorrente seguro (`SKIP LOCKED` + lease com expiração), sem lock global.
- **Papéis de banco:** dono (migrations, executadas pelo job) separado da aplicação, que não tem UPDATE/DELETE no ledger.
- **Fila de eventos:** `MessageGroupId` = `walletId`; como a ordem da outbox é best effort (D44), o consumidor ordena por `walletVersion` e deduplica por `eventId`.
- **Tracing:** spans exportados para stdout/log; `correlationId` propagado como atributo dos spans, alinhado aos logs JSON e aos identificadores do README §12.
- **Teste de carga (k6):** roda contra o Compose com ≥ 3 réplicas; o relatório inclui atraso da outbox e conflitos de concorrência, lidos das métricas na porta administrativa.
- **Eventos exigidos:** `WagerTransactionProcessed`, `WagerTransactionRejected`, `WalletBalanceChanged` e `WagerTransactionPendingReference`. Nenhum extra.
