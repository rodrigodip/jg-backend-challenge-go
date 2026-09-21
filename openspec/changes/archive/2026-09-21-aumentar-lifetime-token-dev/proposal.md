# Proposal

## Why

Sessões manuais de exploração/avaliação sofrem de expiração precoce dos tokens: os clientes interativos do realm têm `access.token.lifespan: 300` (5 min), então a bateria de chamadas toma `401` no meio do fluxo e renovar um token deixa o outro expirar. O enunciado exige rejeitar credenciais expiradas, e isso já é servido pelo cliente dedicado `test-short-lived` (20s). Subir o lifetime dos clientes interativos para 1h é uma decisão de ambiente local de dev — segura e que melhora a experiência do avaliador (sessão manual fluida), sem tocar em requisito do sistema.

## What Changes

- `deployments/keycloak/realm.json`: `provider-a` e `provider-b` mudam `access.token.lifespan` de `300` para `3600`; `internal-service` ganha `access.token.lifespan: 3600`; `test-short-lived` permanece em `20` (dedicado ao teste obrigatório de credencial expirada).
- Documentação da mudança e do racional:
  - `README.md` (§ Exemplos autenticados): nota de uma linha sobre o tempo de vida dos tokens.
  - `ARCHITECTURE.md` (§9 Auth e autorização): menção aos lifetimes dos clientes.
  - `docs/adr/0010-auth-keycloak-client-credentials.md`: nota de revisão registrando a mudança e por que não enfraquece a exigência de rejeição de expirado.

Sem mudança de comportamento especificado (config de dev + documentação), portanto `skip_specs: true`.

## Capabilities

### New Capabilities

- Nenhuma.

### Modified Capabilities

- Nenhuma. Mudança de configuração de provisionamento e documentação; sem delta de spec (`skip_specs: true`).

## Impact

- `deployments/keycloak/realm.json` — tempo de vida dos tokens dos clientes interativos.
- `README.md`, `ARCHITECTURE.md`, `docs/adr/0010-*.md` — notas de documentação.
- Ambiente: reiniciar o container do Keycloak para re-importar o realm e validar `exp - iat == 3600`.
- Nenhum teste de integração afetado: o teste de token expirado usa `test-short-lived` (20s), e os demais renovam token por teste.