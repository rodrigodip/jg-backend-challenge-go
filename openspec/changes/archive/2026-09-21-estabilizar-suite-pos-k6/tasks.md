# Tasks

## 1. Harness determinístico no Makefile

- [x] 1.1 Adicionar purge das 4 filas SQS locais ao alvo `test-integration` e verificar com `grep PurgeQueue Makefile` que as URLs fixas apontam para `localhost:4566`
- [x] 1.2 Trocar o `go test` do alvo para `-count=1` e verificar que uma segunda invocação seguida não retorna `(cached)`
- [x] 1.3 Religar `consumer workers` ao final do alvo preservando o exit code da suite e verificar com `docker compose ps` que os 6 containers voltam a `running` após falha simulada

## 2. Helper de drenagem resiliente

- [x] 2.1 Ajustar `drainEvents` em `tests/sqs_workers_test.go` para drenar até 2 rodadas vazias consecutivas com teto de tempo e verificar que `TestOutboxBrokerDelivery` passa isolado com backlog simulado na fila
- [x] 2.2 Rodar `go vet -tags integration ./...` e `gofmt -l .` e verificar saída limpa após a edição do helper

## 3. Documentação da sequência

- [x] 3.1 Documentar a sequência `make k6` → purge → `docker compose up -d` no README § testes e verificar que os comandos copiados do README executam sem erro num checkout limpo
- [x] 3.2 Validar a change com `openspec validate --change estabilizar-suite-pos-k6` e verificar 0 erros

## 4. Verificação ponta a ponta

- [x] 4.1 Rodar `make k6` seguido de `make test-integration` na mesma sessão e verificar suite `ok` e `make health` com 6 containers saudáveis
