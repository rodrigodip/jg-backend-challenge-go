# Tasks

## 1. Dreno prévio no alvo

- [x] 1.1 Reordenar `make test-integration` para começar com `docker compose up -d consumer workers` e verificar com `docker compose ps` que os 6 containers ficam `running`
- [x] 1.2 Adicionar poll bounded do outbox do Postgres (count 0, 60 iterações × 5s, `tr -d '[:space:]'`) antes de parar os workers e verificar que, com outbox vazio, o alvo não espera iterações
- [x] 1.3 Manter purge SQS, `-count=1` e religamento com exit code preservado e verificar que a ordem final é up → dreno → stop → purge → test → up

## 2. Falha de setup explícita

- [x] 2.1 Simular outbox que não drena (bloquear workers ou poluir linhas) e verificar que o alvo falha com mensagem clara de timeout (não `FAIL` de teste) dentro do teto

## 3. Documentação

- [x] 3.1 Atualizar comentário do alvo e README § testes informando o dreno do outbox do Postgres e verificar que os comandos copiados do README executam sem erro
- [x] 3.2 Rodar `go build ./...`, `go vet ./...`, `go vet -tags integration ./...` e `gofmt -l .` e verificar saída limpa

## 4. Verificação ponta a ponta

- [x] 4.1 Reproduzir as condições do `Error 1`: `make k6` → `docker compose up -d` → `make test-integration` na mesma sessão com log integral e verificar suite `ok` + `make health` 6/6
- [x] 4.2 Validar a change com `openspec validate "drenar-outbox-postgres-pre-suite" --type change` e verificar 0 erros