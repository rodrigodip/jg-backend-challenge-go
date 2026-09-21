# Tasks

## 1. Configuração do realm

- [x] 1.1 Editar `deployments/keycloak/realm.json`: `provider-a` e `provider-b` com `access.token.lifespan: 3600` e `internal-service` ganhando `access.token.lifespan: 3600`, mantendo `test-short-lived` em `20`; verificar com `python3 -c "import json;d=json.load(open('deployments/keycloak/realm.json'));print({c['clientId']:c.get('attributes',{}).get('access.token.lifespan') for c in d['clients']})"` mostrando os valores esperados
- [x] 1.2 Re-importar o realm: `docker compose restart keycloak` e aguardar healthy; verificar com `make health`
- [x] 1.3 Validar os lifetimes emitidos: renovar token de `provider-a` e conferir `exp - iat == 3600` no decode; renovar `test-short-lived` (secret `test-secret`) e conferir `exp - iat == 20`

## 2. Documentação

- [x] 2.1 Adicionar em `README.md` (§ Exemplos autenticados) nota de uma linha: tokens dos clientes interativos duram 1h; `test-short-lived` (20s) valida a rejeição de credencial expirada; verificar que a nota aparece no texto final
- [x] 2.2 Atualizar `ARCHITECTURE.md` §9 para citar os lifetimes dos clientes interativos (1h) e o `test-short-lived` (20s) como mecanismo da exigência de expirado; verificar no texto final
- [x] 2.3 Adicionar em `docs/adr/0010-auth-keycloak-client-credentials.md` uma nota de revisão (padrão "Revisão YYYY-MM-DD") registrando a mudança de lifetime e o racional (segurança da exigência preservada pelo `test-short-lived`); verificar o formato consistente com a revisão anterior

## 3. Verificação final

- [x] 3.1 Rodar `go test ./...` e `go test -race ./...`; verificar que passam (nenhuma mudança de código esperada)
- [x] 3.2 Rodar `go vet -tags integration ./tests/`; verificar que não há erros
- [x] 3.3 Rodar `make test-integration`; verificar que a suite, incluindo `auth_oidc_test.go` (rejeição de token expirado com `test-short-lived`), passa com o realm reimportado