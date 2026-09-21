# ADR-0010: Auth OIDC com Keycloak

- Status: aceito
- IDs: D31(revisto), D32–D36, D41, D45–D51, D54, D55 · Base: REQUISITOS.md §§2, 9, 12 · Specs: `idempotency-contracts`, `auth-observability`

## Contexto

Auth obrigatória via IdP externo; sem senhas próprias nem emissão de tokens;
provedores só nas próprias transações; carteira só para serviço interno;
mensageria sob credenciais do broker; health público; readiness de PG+SQS.
O pré-processamento usava 422 para corrigível e definitivo — indistinguíveis.

## Decisão

Keycloak com realm importado, `client_credentials`
(`provider-a`, `provider-b`, `internal-service`, `test-short-lived` de 20s),
`providerId` de claim mapeado, JWT via JWKS (30s leeway), roles
`provider`/`internal`. Contrato: `400` corrigível sem persistência, `422`
`REJECTED` persistido com evento, `500` `FAILED` sem evento, `409` conflito,
`202` pendente, `503` + `Retry-After`; `403 PROVIDER_FORBIDDEN` sem dados
fora do escopo. SQS com credenciais por papel (não numéricas) + policies;
enforcement do emulador não garantido. Logs JSON com correlationIds (sem
payload financeiro), Prometheus + OTel stdout, `/metrics` em porta admin,
readiness estrito.

## Consequências

- Isolamento auditável inclusive em replays; sem efeito financeiro em 401/403.
- `BET_HAS_ACTIVE_WIN`, `IDEMPOTENCY_CONFLICT`, `PROVIDER_FORBIDDEN`
  completam a taxonomia (corrigível / definitivo / `INTERNAL_PERMANENT_FAILURE`).

## Revisão 2026-09-20 (split-horizon iss)

O `iss` passou a vincular o **caminho do realm** (`/realms/wallet`), não o
host: o Keycloak assina o `iss` a partir do host da requisição e o deployment
é alcançável por dois aliases do mesmo realm (`keycloak:8080` in-network,
`localhost:8081` no host). A assinatura sobre as chaves do realm continua
sendo a garantia de vínculo — realm distinto (`/realms/other`) falha
fechado. Sem isso, os exemplos autenticados do README.md (solução) seriam 401
a partir do host (verificado em checkout limpo, task 7.2).

## Revisão 2026-09-21 (token lifetime de dev)

Os clientes interativos (`provider-a`, `provider-b`, `internal-service`)
passaram de `access.token.lifespan: 300` para `3600` (1h) no realm importado,
para que sessões manuais de exploração/avaliação não morram com `401` no meio
da bateria. Isso **não** enfraquece a exigência de rejeição de credencial
expirada (REQUISITOS.md §13): o mecanismo da prova é o cliente dedicado
`test-short-lived` (20s), inalterado, usado por `tests/auth_oidc_test.go`. O
realm importa o `realm.json` apenas em container novo (`--import-realm` não
sobrescreve clientes já existentes); ao alterar lifetimes, recrie o container
do Keycloak e reinicie a api para renovar o JWKS (chaves regeneradas).
