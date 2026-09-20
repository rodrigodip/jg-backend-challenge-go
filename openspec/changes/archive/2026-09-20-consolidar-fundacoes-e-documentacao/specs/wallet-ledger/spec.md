# Spec Delta

## Purpose

Garante que a carteira seja a raiz do agregado financeiro, com saldo nunca negativo, historico imutavel e reconciliacao confiavel.

## ADDED Requirements

### Requirement: Identidade e criacao de carteira

O sistema SHALL identificar unicamente uma carteira por `(playerId, currency)`, criar via `POST /wallets` com `playerId` e `initialBalance`, responder com `id`, `playerId`, `balance` e `version`, iniciar `version` em 1, e SHALL responder conflito ao tentar segunda carteira para o mesmo par sem exigir header de idempotencia.

#### Scenario: Abertura com saldo positivo

- **WHEN** cliente interno abre carteira com `1000.00 BRL`
- **THEN** o sistema cria a carteira v1, registra `OPENING` em `PROCESSED` com lancamento de credito e eventos `WagerTransactionProcessed` + `WalletBalanceChanged` no mesmo commit

#### Scenario: Abertura zero e duplicada

- **WHEN** abertura com `0.00` ou repeticao do mesmo `(playerId, currency)`
- **THEN** o sistema cria a carteira v1 sem `OPENING`, ledger ou eventos no caso zero, e responde conflito no caso duplicado

### Requirement: Debito e credito sob controle do agregado

O sistema SHALL debitar somente com saldo suficiente (`saldo >= 0` sempre), exigir moeda da movimentacao igual a da carteira, incrementar `version` apenas quando houver mudanca de saldo, e SHALL confirmar cada mudanca financeira junto do lancamento correspondente no ledger.

#### Scenario: Disputa de duas apostas de 80 sobre saldo 100

- **WHEN** duas apostas distintas de `80.00 BRL` concorrem na mesma carteira de `100.00 BRL`
- **THEN** o sistema processa exatamente uma, rejeita a outra com `INSUFFICIENT_BALANCE`, termina com saldo `20.00` e um unico debito no ledger

#### Scenario: Carteiras independentes em paralelo

- **WHEN** operacoes de carteiras distintas executam simultaneamente
- **THEN** o sistema avanca ambas sem bloqueio global e sem lost updates

### Requirement: Ledger append-only e imutavel

O sistema SHALL registrar por mudanca de saldo um lancamento imutavel com `walletId`, `transactionId`, direcao, valor, `balanceBefore`, `balanceAfter` e instante, validar `balanceAfter = balanceBefore ± money`, impor unicidade `(walletId, transactionId)`, CHECK de nao-negatividade e de coerencia, e SHALL impedir edicao ou exclusao por schema e permissoes; `LOSS` e operacoes rejeitadas SHALL NOT gerar lancamento nem bump de versao.

#### Scenario: Tentativa de edicao do ledger

- **WHEN** qualquer papel tenta UPDATE ou DELETE em lancamento confirmado
- **THEN** o banco recusa e o historico permanece intacto para auditoria

#### Scenario: LOSS processado sem ledger

- **WHEN** `LOSS` com `"0.00"` e moeda da carteira e processado
- **THEN** o sistema persiste `PROCESSED`, publica `WagerTransactionProcessed`, e nao cria lancamento, nao muda saldo nem versao

### Requirement: Leitura paginada do ledger

O sistema SHALL expor `GET /wallets/:walletId/ledger` com cursor opaco e ordenacao estavel por sequencia por carteira (versao pos-mudanca, unica por `(walletId, sequencia)`), com limite padrao e maximo documentados.

#### Scenario: Paginacao estavel sob concorrencia

- **WHEN** cliente pagina o ledger enquanto novos creditos chegam
- **THEN** o sistema devolve paginas sem pular nem duplicar entradas ja vistas

### Requirement: Reconciliacao consistente e nao-destrutiva

O sistema SHALL reconstruir o saldo a partir do ledger incluindo a abertura, comparar com o saldo armazenado numa visao consistente (transacao somente-leitura), responder `storedBalance`, `calculatedBalance`, `difference` (armazenado menos reconstruido), `consistent` e `checkedEntries`, e SHALL reportar divergencia na resposta, em log e em metrica sem jamais alterar o saldo; acesso restrito ao servico interno.

#### Scenario: Carteira consistente

- **WHEN** reconciliacao roda sobre carteira integra
- **THEN** o sistema responde `difference 0.00`, `consistent true` e `checkedEntries` igual ao numero de entradas com saldo

#### Scenario: Divergencia detectada

- **WHEN** stored e reconstruido divergem
- **THEN** o sistema responde `consistent false` com a diferenca, emite log estruturado e incrementa a metrica de divergencia
