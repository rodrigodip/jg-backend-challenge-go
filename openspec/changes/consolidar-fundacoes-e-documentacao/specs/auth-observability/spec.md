# Spec Delta

## Purpose

Garante que so identidades verificadas operem dinheiro, que provedores vejam apenas o proprio escopo e que tudo seja diagnosticavel e encerrado com seguranca.

## ADDED Requirements

### Requirement: Autenticacao OIDC com Keycloak

O sistema SHALL exigir autenticacao em todos os endpoints de negocio via IdP externo OAuth 2.0/OIDC (Keycloak com realm importado, `client_credentials`), validar JWT localmente por JWKS (assinatura, `iss`, `aud`, `exp`, `nbf`, tolerancia de relogio de 30s), rejeitar credencial ausente, invalida ou expirada com `401`, e SHALL extrair `providerId` de claim com protocol mapper (nunca do corpo sem conferir).

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
