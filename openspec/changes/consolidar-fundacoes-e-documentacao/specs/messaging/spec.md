# Spec Delta

## Purpose

Garante entrega exatamente-uma-vez observavel nas bordas assincronas, publicando eventos somente apos commit e sobrevivendo a falhas e reinicios.

## ADDED Requirements

### Requirement: Filas FIFO provisionadas com DLQ

O sistema SHALL provisionar `wager-transactions.fifo` e `wager-transactions-dlq.fifo` com redrive (`maxReceiveCount 5`), mais `wager-events.fifo` de saida com DLQ, via hooks de init, e SHALL documentar `MessageGroupId = walletId` (ordem por carteira) e `MessageDeduplicationId = messageId` na entrada e `eventId` na saida.

#### Scenario: Redrive apos esgotar recebimentos

- **WHEN** mensagem transitoria falha ate `maxReceiveCount`
- **THEN** o broker a move para a DLQ preservando identidade para inspecao

### Requirement: Consumidor com caso de uso compartilhado

O sistema SHALL compartilhar caso de uso e garantias de idempotencia financeira entre HTTP e SQS, usar o `messageId` do envelope como identidade duravel com verificacao de hash em reentregas, consumir em batch de ate 10 com ate 10 handlers concorrentes por instancia e visibility timeout de 30s, remover da fila somente apos commit do tratamento duravel, e SHALL validar concorrencia cruzada HTTP x SQS nos testes.

#### Scenario: Interrupcao entre commit e remocao

- **WHEN** consumidor cai apos o commit e antes de deletar a mensagem
- **THEN** a reentrega e reconhecida como duplicata pela inbox e nenhum movimento se repete

### Requirement: Inbox transacional

O sistema SHALL registrar inbox com unicidade `(consumerName, messageId)`, hash, recebimento e conclusao, compartilhar a transacao SQL do tratamento (dominio, ledger, eventos) e concluir a inbox da referencia pendente apos persistir `PENDING_REFERENCE`, deixando a continuidade com o worker de referencias.

#### Scenario: Reentrega com mesmo messageId e hash

- **WHEN** SQS redelivery repete `messageId` com conteudo identico
- **THEN** o sistema retorna o resultado persistido sem reprocessar

#### Scenario: Mesmo messageId com hash divergente

- **WHEN** reentrega traz mesmo `messageId` com conteudo diferente
- **THEN** o sistema envia a DLQ com metadados do erro e remove a original, sem aplicar financeiro

### Requirement: Destino de rejeicoes, transitorias e invalidas

O sistema SHALL remover apos commit rejeicoes definitivas de negocio (`REJECTED`), SHALL retentar transitorias com `ChangeMessageVisibility` em backoff exponencial com jitter (base 1s, teto 30s), e SHALL enviar a DLQ com `MessageGroupId`, `MessageDeduplicationId` e metadados do erro as mensagens invalidas, erros permanentes e esgotadas, removendo a original.

#### Scenario: Mensagem malformada

- **WHEN** corpo SQS e invalido e nunca sera processavel
- **THEN** o sistema a encaminha a DLQ documentada em vez de retentar indefinidamente

### Requirement: Outbox transacional com publishers concorrentes

O sistema SHALL confirmar atomicamente estado, saldo, ledger, inbox e eventos conforme aplicavel, publicar eventos SOMENTE apos o commit de origem via worker separado, suportar multiplos publishers com claim `SKIP LOCKED` + lease com expiracao e backoff, recuperar trabalho abandonado por outra instancia, e SHALL preservar `eventId` em republicacoes; ordem e best-effort e o consumidor deduplica por `eventId` e ordena por `walletVersion`.

#### Scenario: Queda entre commit e publicacao

- **WHEN** instancia cai apos confirmar a transacao e antes de publicar
- **THEN** outra instancia assume o registro pendente e publica com o mesmo `eventId`, sem duplicar efeito financeiro

#### Scenario: Dois publishers disputando a outbox

- **WHEN** dois workers tentam o mesmo lote pendente
- **THEN** exatamente um publica cada evento e o outro observa o claim sem duplicar entrega

### Requirement: Eventos tipados com envelope estavel

O sistema SHALL emitir `WagerTransactionProcessed` (inclui `LOSS`), `WagerTransactionRejected`, `WalletBalanceChanged` (so em mudanca efetiva, com `walletId`, `transactionId`, `direction`, `money`, `balanceBefore`, `balanceAfter`, `walletVersion`) e `WagerTransactionPendingReference`, com envelope `eventId`, `eventType`, `aggregateId = walletId`, `correlationId`, `causationId` opcional, `occurredAt` UTC RFC 3339, `version` e `data` tipado como snapshot imutavel em strings decimais; nenhum evento para `FAILED`; eventos de abertura `OPENING` nao exigem metadados externos inaplicaveis.

#### Scenario: Abertura positiva gera dois eventos atomicos

- **WHEN** carteira abre com saldo positivo
- **THEN** o mesmo commit grava carteira, `OPENING`, ledger e outbox com `Processed` + `BalanceChanged` versao 1

### Requirement: Shutdown gracioso do consumidor

O sistema SHALL, em `SIGTERM`, parar de buscar trabalho, concluir o em andamento dentro de 30s ou liberar sua visibilidade para reentrega segura, sem perder evento confirmado nem aplicar duplicata.

#### Scenario: SIGTERM durante processamento

- **WHEN** sinal chega com mensagem em voo proximo do timeout
- **THEN** o sistema conclui ou libera visibilidade e a mensagem e reentregue sem corromper o financeiro
