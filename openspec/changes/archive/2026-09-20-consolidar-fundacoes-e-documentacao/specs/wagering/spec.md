# Spec Delta

## Purpose

Define o ciclo de vida das apostas e as regras de referencias e reversoes que preservam a coerencia financeira do seamless wallet.

## ADDED Requirements

### Requirement: Tipos e movimentacao

O sistema SHALL suportar tipos externos `BET` (debito, exige positivo e saldo), `WIN` (credito, exige positivo, referencia opcional), `LOSS` (sem movimento, exige `"0.00"`), `REFUND` (credito integral de `BET` processada) e `ROLLBACK` (movimento contrario integral de `BET`/`WIN`/`REFUND` processada); SHALL rejeitar `OPENING` vindo de HTTP ou SQS com `OPENING_NOT_ALLOWED`.

#### Scenario: BET sem saldo

- **WHEN** `BET` de `80.00` chega em carteira com `20.00`
- **THEN** o sistema persiste `REJECTED` com `INSUFFICIENT_BALANCE` e evento de rejeicao, sem ledger

#### Scenario: ROLLBACK que quebraria o saldo

- **WHEN** reversao exigiria debitar alem do disponivel
- **THEN** o sistema persiste `REJECTED` com `ROLLBACK_INSUFFICIENT_BALANCE`, codigo distinto do de aposta sem saldo

### Requirement: Maquina de estados com replay

O sistema SHALL iniciar toda transacao em `PENDING`, permitir `PENDING -> PROCESSED | REJECTED | PENDING_REFERENCE | FAILED` e `PENDING_REFERENCE -> PROCESSED | REJECTED | FAILED`, proibir transicao a partir de estado terminal, e SHALL responder replay consultando o resultado persistido (incluindo saldo observado original) sem reaplicar movimento; `FAILED` e reservado a falha permanente de infraestrutura, nunca a regra de negocio.

#### Scenario: Replay apos novas movimentacoes

- **WHEN** operacao `PROCESSED` com saldo observado `975.00` e reenviada apos a carteira mudar para `500.00`
- **THEN** o sistema devolve o resultado original com saldo `975.00` e `idempotentReplay true` sem tocar no saldo atual

#### Scenario: Terminal nao transita

- **WHEN** evento tardio tenta mover transacao `PROCESSED`, `REJECTED` ou `FAILED`
- **THEN** o sistema recusa a transicao e mantem o estado e o financeiro intactos

### Requirement: Referencia obrigatoria e concordancia em reversoes

O sistema SHALL exigir `referenceExternalTransactionId` em `REFUND`/`ROLLBACK`, resolve-lo por `(providerId, referenceExternalTransactionId)`, e SHALL exigir concordancia em provedor, jogador, carteira, moeda e rodada, valor igual ao referenciado (sem parcial), e `playerId` da operacao igual ao da carteira.

#### Scenario: Valor divergente da referencia

- **WHEN** `REFUND` de `20.00` referencia `BET` de `25.00`
- **THEN** o sistema persiste `REJECTED` com `AMOUNT_MISMATCH`

#### Scenario: Moeda ou rodada divergente

- **WHEN** operacao e referencia divergem em moeda, carteira, jogador ou rodada
- **THEN** o sistema persiste `REJECTED` com o `REFERENCE_*_MISMATCH` correspondente

### Requirement: Matriz de reversoes anti-overpayment

O sistema SHALL permitir `REFUND` somente de `BET` processada e `ROLLBACK` de `BET`/`WIN`/`REFUND` processada; SHALL negar qualquer reversao de `LOSS`, `OPENING` ou transacao nao processada, `REFUND` de `WIN`/`REFUND`/`ROLLBACK`, e `ROLLBACK` de `ROLLBACK`; SHALL permitir no maximo uma reversao direta bem-sucedida por `BET` (`ALREADY_REVERSED` nas seguintes); e SHALL negar reversao de `BET` que possua `WIN` `PROCESSED` dependente ainda nao revertido (`BET_HAS_ACTIVE_WIN`).

#### Scenario: Segunda reversao da mesma BET

- **WHEN** `BET` ja revertida por `REFUND` recebe `ROLLBACK` direto
- **THEN** o sistema persiste `REJECTED` com `ALREADY_REVERSED`

#### Scenario: Refund com WIN ativo

- **WHEN** `REFUND` mira `BET` que possui `WIN` processado dependente nao revertido
- **THEN** o sistema persiste `REJECTED` com `BET_HAS_ACTIVE_WIN`, impedindo stake de volta + premio

#### Scenario: Rollback de refund como void

- **WHEN** `ROLLBACK` mira `REFUND` processado
- **THEN** o sistema debita o valor integral, marca o `REFUND` como revertido e emite ledger e eventos da nova transacao

### Requirement: Referencia ainda indisponivel com expiracao

O sistema SHALL persistir como `PENDING_REFERENCE` com evento `WagerTransactionPendingReference` quando a referencia nao chegou, tentar com backoff exponencial com jitter sobrevivendo a reinicio, reavaliar imediatamente na transacao em que a referencia vira `PROCESSED` mais polling de fallback, aguardar referencia ainda `PENDING`/`PENDING_REFERENCE`, rejeitar com `REFERENCE_NOT_PROCESSED` se a referencia terminou `REJECTED`/`FAILED`, e ao esgotar TTL de 60s ou 5 tentativas finalizar `REJECTED` com `REFERENCE_NOT_FOUND` e evento de rejeicao.

#### Scenario: Reversao antes da referencia com resolucao

- **WHEN** `REFUND` chega antes da `BET` e a `BET` processa em seguida
- **THEN** o sistema resolve a pendencia e processa o `REFUND` sem intervento manual

#### Scenario: Referencia que nunca chega

- **WHEN** TTL ou tentativas esgotam sem a referencia
- **THEN** o sistema finaliza `REJECTED` com `REFERENCE_NOT_FOUND`, publica rejeicao e permite acompanhamento por consulta

### Requirement: WIN com referencia opcional validada

O sistema SHALL aceitar `WIN` sem referencia, e quando informada SHALL exigir que aponte para `BET` `PROCESSED` nao revertida da mesma rodada, jogador, carteira e moeda; se indisponivel SHALL seguir `PENDING_REFERENCE`; se a `BET` terminar sem sucesso SHALL rejeitar com `REFERENCE_NOT_PROCESSED`.

#### Scenario: WIN referenciando BET revertida

- **WHEN** `WIN` referencia `BET` ja revertida
- **THEN** o sistema persiste `REJECTED` com `REFERENCE_REVERSED`
