# Design

## Context

Sessões manuais morrem com `401` porque os tokens dos clientes interativos expiram em 5 min (`realm.json` define `access.token.lifespan: 300` para `provider-a`/`provider-b`; `internal-service` herda o default do Keycloak, também 300s). O teste obrigatório de credencial expirada usa o cliente dedicado `test-short-lived` (20s). Ver `proposal.md - Why` para motivação.

## Goals / Non-Goals

**Goals:**
- Tokens dos clientes interativos com vida útil que cubra uma sessão manual de avaliação (1h).
- Manter intacta a prova de rejeição de credencial expirada (exigência do §13).
- Documentar a mudança onde o avaliador olha (README, ARCHITECTURE §9, ADR-0010).

**Non-Goals:**
- Não alterar a validação JWT do sistema (JWKS, `exp`, leeway de 30s) — só a emissão local no realm de dev.
- Não mexer em `test-short-lived` (permanece 20s).
- Nenhuma mudança de requisito (skip_specs).

## Decisions

**1. Lifetime por cliente, não default do realm.**
Manter os overrides por cliente (`access.token.lifespan`) em vez de subir o default global: `provider-a`/`provider-b` → `3600`, `internal-service` → `3600`, `test-short-lived` → `20`. Alternativa considerada: subir só o default do realm — rejeitada porque `test-short-lived` poderia herdar um default alto e quebrar o teste de expirado; override explícito por cliente é mais legível e à prova de futuro.

**2. Racional de segurança documentado.**
O enunciado exige "rejeição de credenciais ausentes, inválidas ou expiradas". A 1h vale para os clientes interativos de dev; a rejeição de expirado é provada pelo `test-short-lived` (20s) em `tests/auth_oidc_test.go`. A nota de doc deixa isso explícito para o avaliador não ler 1h como relaxamento.

**3. Re-import do realm.**
O Keycloak sobe com `--import-realm` (`docker-compose.yml`) montando `deployments/keycloak/realm.json`; mudar o arquivo e reiniciar o container reaplica o realm. Validação: renovar token de `provider-a` e conferir `exp - iat == 3600`; renovar `test-short-lived` e conferir `20`.

## Risks / Trade-offs

- [Re-import não aplicar o novo valor] Keycloak pode pular recursos já existentes em certas versões. → Mitigação: validar `exp - iat` após o restart; se não aplicar, recriar o container do keycloak (volume de dados do realm é descartável em dev).
- [Leitura de "enfraquecimento" pelo avaliador] 1h pode parecer um relaxamento da exigência de expirado. → Mitigação: nota em README/ARCHITECTURE/ADR explicitando o `test-short-lived` como mecanismo da exigência.

## Migration Plan

Local/commit: editar `realm.json`, `docker compose restart keycloak`, validar. Rollback: reverter o arquivo + restart (nenhum dado de produção envolvido).

## Open Questions

Nenhuma que mude spec, abordagem ou tarefas.