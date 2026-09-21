# Spec Delta

## ADDED Requirements

### Requirement: Consulta de transacao por provedor e id externo

O sistema SHALL expor `GET /providers/:providerId/wagering/transactions/:externalTransactionId` para o papel `provider` consultar a propria transacao pelo id externo, devolvendo o mesmo corpo e codigos de `GET /wagering/transactions/:transactionId`; SHALL exigir que o `providerId` do caminho corresponda ao da identidade autenticada e que a transacao pertenca ao provedor, respondendo `403 PROVIDER_FORBIDDEN` sem dados em divergencia de caminho ou acesso a transacao de outro provedor; e SHALL responder `404` quando nao existir transacao para aquele provedor.

#### Scenario: Provedor consulta a propria transacao pelo id externo

- **WHEN** provedor autenticado consulta `GET /providers/provider-a/wagering/transactions/ext-123` referente a transacao `PROCESSED` de sua autoria
- **THEN** o sistema responde `200` com o corpo identico ao de `GET /wagering/transactions/:transactionId` (incluindo `transactionId`, `status`, `idempotentReplay`, `balance` e demais campos aplicaveis)

#### Scenario: Caminho divergente da identidade

- **WHEN** provedor A consulta `GET /providers/provider-b/wagering/transactions/ext-123`
- **THEN** o sistema responde `403 PROVIDER_FORBIDDEN` sem dados e sem efeito financeiro

#### Scenario: Transacao de outro provedor

- **WHEN** provedor A consulta, usando seu proprio caminho, id externo que pertence a transacao do provedor B
- **THEN** o sistema responde `404` sem vazar dados (a resolucao por `(providerId, externalTransactionId)` e intrinsecamente restrita ao provedor e nao revela a existencia da transacao de B)

#### Scenario: Transacao inexistente para o provedor

- **WHEN** provedor consulta id externo desconhecido
- **THEN** o sistema responde `404` sem expor dados