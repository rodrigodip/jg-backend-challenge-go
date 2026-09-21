# Auth Observability Specification

## Purpose

Garante que so identidades verificadas operem dinheiro, que provedores vejam apenas o proprio escopo e que tudo seja diagnosticavel e encerrado com seguranca.

## Requirements

### Requirement: Autenticacao OIDC com Keycloak

O sistema SHALL exigir autenticacao em todos os endpoints de negocio via IdP externo OAuth 2.0/OIDC (Keycloak com realm importado, `client_credentials`), validar JWT localmente por JWKS (assinatura, `iss` vinculado ao caminho do realm — o host pode variar entre aliases do mesmo deployment, p.ex. `keycloak:8080` in-network e `localhost:8081` no host —, `aud`, `exp`, `nbf`, tolerancia de relogio de 30s), rejeitar credencial ausente, invalida ou expirada com `401`, e SHALL extrair `providerId` de claim com protocol mapper (nunca do corpo sem conferir).

#### Scenario: Token expirado

- **WHEN** requisicao chega com token de vida curta ja expirado
- **THEN** o sistema responde `401` sem efeito financeiro e sem expor dados

### Requirement: Autorizacao por papel e escopo

O sistema SHALL aplicar roles `provider` (enviar operacoes e consultar apenas as proprias transacoes, `providerId` do corpo/caminho igual ao da identidade sob pena de `403 PROVIDER_FORBIDDEN` sem dados) e `internal` (carteiras, ledger, reconciliacao e leitura de qualquer transacao incluindo `OPENING`), com health publico e metricas em porta administrativa separada.

#### Scenario: Provider lendo transacao alheia

- **WHEN** provedor A consulta transacao do provedor B ou envia corpo com outro `providerId`
- **THEN** o sistema responde `403` sem vazar dados e sem registrar financeiro

### Requirement: Credenciais e politicas do broker

O sistema SHALL controlar acesso a mensageria por credenciais separadas por papel (produtor, consumidor, publisher, com chaves nao-numericas na mesma conta do emulador) e queue policies, documentar o limite de enforcement do emulador, e SHALL preservar sempre as validacoes de dominio no consumidor.

#### Scenario: Consumidor sem credencial valida

- **WHEN** worker tenta ler a fila com credencial errada
- **THEN** o broker nega e nenhum tratamento financeiro ocorre

### Requirement: Observabilidade diagnostica

O sistema SHALL emitir logs JSON com `correlationId`, `messageId`, `transactionId`, `walletId` e `providerId` quando disponiveis, sem credenciais, dados sensiveis ou payloads financeiros completos, e SHALL expor metricas de resultados por status, duplicatas, retries, DLQ, conflitos de concorrencia, atraso da outbox, latencia de processamento e divergencias de reconciliacao, mais tracing OpenTelemetry com exportador stdout/log e `correlationId` como atributo de span.

#### Scenario: Rastreio ponta a ponta

- **WHEN** operacao atravessa HTTP, dominio, outbox e consumidor
- **THEN** todos os logs e spans compartilham o mesmo `correlationId` permitindo reconstruir o fluxo

### Requirement: Health e readiness estritos

O sistema SHALL expor `GET /health/live` (processo) e `GET /health/ready` (PostgreSQL e SQS disponiveis) publicos na porta principal, com readiness estrito que falha se qualquer dependencia critica estiver indisponivel.

#### Scenario: SQS indisponivel

- **WHEN** broker cai e readiness e consultado
- **THEN** o sistema responde nao-pronto, sinalizando ao orquestrador sem mascarar a falha

### Requirement: Composicao Fx e ciclo de vida

O sistema SHALL compor configuracao, conexoes, repositorios, casos de uso, handlers e workers por `fx.Module`/`fx.Provide`/`fx.Invoke` com dominio independente de Fx/HTTP/SQS/persistencia, validar configuracao e dependencias na inicializacao, gerenciar workers com cancelamento, prazos e termino observavel, e SHALL no shutdown interromper novas entradas, concluir ou liberar o trabalho em voo e fechar dependencias apos os componentes que as usam.

#### Scenario: Teste de composicao

- **WHEN** suite de integracao sobe e desce a aplicacao Fx
- **THEN** inicio e encerramento ocorrem sem vazamento de workers, conexoes ou goroutines

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
