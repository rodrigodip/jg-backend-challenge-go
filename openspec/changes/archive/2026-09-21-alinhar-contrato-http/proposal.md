# Proposal

## Why

O contrato HTTP implementado diverge dos exemplos literais do `REQUISITOS.md` §9: a rota de leitura por provedor `GET /providers/:providerId/wagering/transactions/:externalTransactionId` não existe (gap funcional real — o provedor conhece o `externalTransactionId`, não o UUID interno), e as demais divergências de forma (`money` objeto vs `amount`/`currency` flat, `initialBalance` vs `initialAmount`, `id` vs `walletId`, reconciliação como `GET`) não estão documentadas como interpretação adotada. Um avaliador que dirija a API usando os exemplos do §9 literal receberia `404`/`400` e poderia ler a solução como "spec não implementada". Escopo decidido (A+B): fechar o gap funcional da rota e documentar as divergências de forma como interpretação.

## What Changes

- Adiciona `GET /providers/:providerId/wagering/transactions/:externalTransactionId` para o provedor consultar a própria transação pelo id externo, reutilizando a resolução `(providerId, externalTransactionId)` já existente (`FindByProviderExternal`), com isolamento: `providerId` do caminho deve igualar a identidade e a transação deve pertencer ao provedor, senão `403 PROVIDER_FORBIDDEN` sem dados; transação inexistente devolve `404`. Corpo de resposta idêntico ao de `GET /wagering/transactions/:transactionId`.
- Documenta em `ARCHITECTURE.md` as divergências do §9 tratadas como interpretação adotada: forma flat `amount`/`currency` no wire (o hash canônico de idempotência já normaliza para a forma `money` do §9, preservando equivalência HTTP/SQS), `initialAmount` + `currency` em vez de `initialBalance`, `walletId` em vez de `id`, e reconciliação como `GET` por ser leitura pura (o próprio §9 proíbe que a reconciliação altere saldo).

## Capabilities

### New Capabilities

- Nenhuma.

### Modified Capabilities

- `auth-observability`: o escopo de leitura do papel `provider` passa a incluir explicitamente a consulta pela própria transação via id externo na rota `GET /providers/:providerId/wagering/transactions/:externalTransactionId`, com `403 PROVIDER_FORBIDDEN` sem dados para caminho divergente da identidade ou transação de outro provedor e `404` para transação inexistente.

## Impact

- `internal/adapters/http/handler.go` e `engine.go`: nova rota no grupo protegido.
- `internal/adapters/http/handler.go`: handler de consulta por `(providerId, externalTransactionId)`.
- `tests/http_contract_test.go`: cobertura de integração da nova rota (leitura do dono, `403` de outro provedor sem dados, `404`).
- `README.md`: exemplo de chamada da nova rota.
- `ARCHITECTURE.md`: seção documentando as divergências do contrato §9 e sua justificativa.