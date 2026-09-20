# ADR-0010: Auth OIDC com Keycloak

- Status: aceito
- IDs: D31(revisto), D32–D36, D41, D45–D51, D54, D55 · Base: README §§2, 9, 12 · Specs: `idempotency-contracts`, `auth-observability`

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
