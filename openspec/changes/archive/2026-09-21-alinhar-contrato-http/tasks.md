# Tasks

## 1. Rota provider-scoped de leitura por id externo

- [x] 1.1 Extrair a montagem do corpo de `getTransaction` (`internal/adapters/http/handler.go`) para um helper `transactionBody(rec *ports.TxRecord) gin.H` e refatorar `getTransaction` para usá-lo; verificar que `go build ./...` compila e que a leitura por UUID continua com o mesmo corpo nos testes existentes
- [x] 1.2 Implementar o handler `getTransactionByExternal` que lê `:providerId` e `:externalTransactionId` do caminho, resolve via `Svc.DB.Tx().FindByProviderExternal(providerID, externalID)`, responde `404 TRANSACTION_NOT_FOUND` quando não encontrado e renderiza o corpo pelo helper compartilhado; verificar com um teste unitário/integração direcionado
- [x] 1.3 Aplicar autorização no handler: papel `provider` exige `providerId` do caminho igual ao da identidade sob pena de `403 PROVIDER_FORBIDDEN` sem dados; papel `internal` lê qualquer provedor sem checagem de caminho; verificar que `provider-a` no caminho de `provider-b` devolve `403` sem corpo de dados
- [x] 1.4 Registrar a rota `GET /providers/:providerId/wagering/transactions/:externalTransactionId` no grupo `protected` de `RegisterRoutes`; verificar que a rota responde e que continua atrás do `AuthMiddleware` (sem token → `401`)

## 2. Cobertura de integração do contrato

- [x] 2.1 Estender `tests/http_contract_test.go` com casos da nova rota: dono lê a própria transação por id externo com `200` e corpo idêntico ao da rota por UUID (comparar `transactionId`, `status`, `balance`); verificar com `make test-integration`
- [x] 2.2 Adicionar casos de isolamento: caminho com `providerId` divergente da identidade → `403` sem dados; id externo de outro provedor consultado pelo próprio caminho → `404`; id externo desconhecido → `404`; papel `internal` lê transação de qualquer provedor → `200`; verificar com `make test-integration`

## 3. Documentação das divergências do contrato

- [x] 3.1 Adicionar em `ARCHITECTURE.md` (subseção sob §3 "Transações e máquina de estados") a seção de divergências do §9: forma flat `amount`/`currency` vs `money` objeto (hash canônico já normaliza para a forma `money`), `initialAmount` + `currency` vs `initialBalance`, `walletId` vs `id`, reconciliação como `GET` por ser leitura pura, e a rota provider-scoped agora implementada; verificar que a seção lista cada divergência com justificativa
- [x] 3.2 Atualizar `README.md` em "Exemplos autenticados" com uma chamada de exemplo da nova rota (`GET /providers/provider-a/wagering/transactions/<ext>`); verificar que o exemplo usa a mesma forma flat do restante do README

## 4. Verificação final

- [x] 4.1 Rodar `gofmt -l .` (sem output), `go vet ./...` e `go vet -tags integration ./...`; verificar que não há erros
- [x] 4.2 Rodar `go test ./...` e `go test -race ./...`; verificar que passam
- [x] 4.3 Rodar `make test-integration`; verificar que toda a suite, incluindo os novos casos da rota, passa com a infraestrutura real