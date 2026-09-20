# Idempotency Contracts Specification

## Purpose

Garante que cada operacao financeira execute exatamente uma vez por todos os canais, com conflito detectavel e replay fiel.

## Requirements

### Requirement: Chave de idempotencia obrigatoria e opaca

O sistema SHALL exigir o header `Idempotency-Key` em `POST /wagering/transactions` (opaco, obrigatorio, nao-vazio, ate 255 caracteres), NUNCA substituir silenciosamente a chave recebida por outra calculada, e SHALL impor unicidade por `(providerId, chave)`; em SQS a chave e `data.idempotencyKey` com deduplicacao adicional pela inbox.

#### Scenario: Chave ausente

- **WHEN** envio HTTP sem `Idempotency-Key`
- **THEN** o sistema rejeita como corrigivel com `INVALID_REQUEST` sem efeito financeiro

### Requirement: Hash deterministico do conteudo

O sistema SHALL persistir SHA-256 sobre JSON canonico (RFC 8785, chaves ordenadas) dos campos `providerId`, `externalTransactionId`, `playerId`, `walletId`, `roundId`, `gameId`, `kind`, `money.amount`, `money.currency` e `referenceExternalTransactionId` (omitido se ausente), excluindo chave e metadados de transporte, gerando hash identico para o mesmo negocio via HTTP e SQS.

#### Scenario: Equivalencia cross-channel

- **WHEN** mesma operacao chega via HTTP e via SQS com chaves e envelopes diferentes mas mesmo conteudo de negocio
- **THEN** o sistema gera o mesmo hash e trata a segunda como replay, sem novo movimento

### Requirement: Replay e conflito

O sistema SHALL, para chave igual e conteudo equivalente, devolver o resultado persistido com `idempotentReplay true` e o saldo observado original; para chave igual e conteudo diferente devolver `409` com `IDEMPOTENCY_CONFLICT`; para `(providerId, externalTransactionId)` igual com chave diferente e conteudo equivalente devolver replay, e com conteudo diferente devolver `409`; e SHALL NUNCA reaplicar movimento financeiro em replay.

#### Scenario: Mesma operacao 50 vezes em paralelo

- **WHEN** a mesma aposta e enviada 50 vezes em paralelo, inclusive cruzando HTTP e SQS
- **THEN** o sistema aplica exatamente um debito e as demais recebem o resultado persistido

### Requirement: Erros corrigiveis nao reservam chave

O sistema SHALL tratar validacoes corrigiveis (`INVALID_REQUEST`, `INVALID_MONEY`, `WALLET_NOT_FOUND`, `WALLET_PLAYER_MISMATCH`, `CURRENCY_MISMATCH`, `REFERENCE_REQUIRED`, `OPENING_NOT_ALLOWED`, `INVALID_AMOUNT_FOR_KIND`) sem persistir transacao nem reservar a chave, permitindo reenvio corrigido.

#### Scenario: Correcao apos erro de carteira

- **WHEN** envio com `walletId` inexistente e reenviado com a carteira correta sob a mesma chave
- **THEN** o sistema processa normalmente em vez de responder conflito

### Requirement: Contrato HTTP distinguivel

O sistema SHALL responder `201` no primeiro processamento concluido, `200` no replay, `202` com `transactionId` e sem saldo quando pendente, `400` em entrada corrigivel estrutural, `422` com corpo persistido (`transactionId`, `status REJECTED`, `failureCode`, `category DEFINITIVE`, `idempotentReplay`, `balance`) em rejeicao de negocio, `409` em conflito de idempotencia ou carteira duplicada, `404` em consulta inexistente, `401` sem credencial ou com credencial invalida/expirada, `403` com `PROVIDER_FORBIDDEN` em acesso a transacao de outro provedor ou `providerId` divergente da identidade, e `503` com `Retry-After` em indisponibilidade transitoria; `LOSS` devolve saldo observado e `PENDING` omite saldo.

#### Scenario: Rejeicao auditavel via API

- **WHEN** cliente consulta transacao `REJECTED`
- **THEN** o sistema devolve status, `failureCode` estavel e saldo observado, permitindo conciliar sem reenviar

#### Scenario: Isolamento entre provedores no replay

- **WHEN** provedor B consulta ou repete operacao do provedor A
- **THEN** o sistema responde `403` sem devolver dados nem produzir efeito financeiro
