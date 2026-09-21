# Design

## Context

O `REQUISITOS.md` §9 lista `GET /providers/:providerId/wagering/transactions/:externalTransactionId` na seção de leitura, mas a solução implementa apenas `GET /wagering/transactions/:transactionId` (por UUID interno). O repositório já expõe `TxRepo.FindByProviderExternal(providerID, externalID)` (`internal/ports/ports.go`, `internal/adapters/postgres/tx_repo.go`) — a mesma resolução usada pelas reversões —, então o acesso aos dados para a rota já existe. Além disso, as demais divergências de forma do §9 estão documentadas apenas implicitamente. Ver `proposal.md - Why` para a motivação completa.

## Goals / Non-Goals

**Goals:**
- Fechar o gap funcional com a rota provider-scoped, sem vazar dados entre provedores.
- Garantir que a nova rota devolva corpo e códigos idênticos à rota de leitura por UUID.
- Deixar explícitas, em `ARCHITECTURE.md`, as divergências de forma do §9 tratadas como interpretação.

**Non-Goals:**
- Não alterar as formas de wire existentes (`amount`/`currency` flat, `initialAmount`, `walletId`, reconciliação como `GET`) — elas ficam como estão e apenas são documentadas.
- Não aceitar ambas as formas de `money` na entrada (opção C) nem adicionar `POST` de reconciliação (opção D).
- Nenhuma mudança de schema, mensageria ou domínio.

## Decisions

**1. Resolução da transação por `FindByProviderExternal`.**
A consulta `WHERE provider_id = ? AND external_transaction_id = ?` é intrinsecamente restrita ao provedor: id externo de outro provedor devolve `nil` (não encontrado) → `404`. Não há como distinguir "transação de B" de "não existe para A", logo não há vazamento. Não é necessário lookup global + checagem de posse. Alternativa considerada: resolver o id externo sem escopo e checar posse para devolver `403` — descartada por exigir consulta não escopada e por não adicionar segurança (404 e 403 não vazam nada aqui).

**2. Autorização da rota.**
A rota entra no grupo `protected` (já atrás do `AuthMiddleware`). No handler:
- papel `provider`: exige `providerId` do caminho == `identity.ProviderID`, senão `403 PROVIDER_FORBIDDEN` sem dados (mesma semântica do `POST /wagering/transactions` e da leitura por UUID);
- papel `internal`: sem checagem de caminho (lê transação de qualquer provedor por id externo, coerente com a leitura por UUID);
- sem identidade válida: `401` já garantido pelo middleware.

**3. Corpo de resposta compartilhado.**
Para cumprir a spec ("mesmo corpo e códigos de `GET /wagering/transactions/:transactionId`"), extrair a montagem atual do corpo de `getTransaction` (`handler.go`) para um helper `transactionBody(rec *ports.TxRecord) gin.H` e reutilizá-lo nas duas rotas. Isso impede que as duas leituras divirjam no futuro. O `404` usa o mesmo `TRANSACTION_NOT_FOUND`.

**4. Documentação das divergências.**
Nova subseção em `ARCHITECTURE.md` (sob §3 "Transações e máquina de estados") enumerando as divergências do §9 e sua justificativa:
- `money` objeto do enunciado vs `amount`/`currency` flat no wire — o hash canônico de idempotência (`hash.go`) já canonicaliza para a forma `money` do §9, preservando equivalência HTTP/SQS;
- `initialBalance` vs `initialAmount` + `currency` na abertura;
- `id` vs `walletId` na resposta de carteira;
- reconciliação como `GET` (o próprio §9 proíbe que a reconciliação altere saldo — leitura pura);
- rota provider-scoped adicionada neste change (eliminando essa divergência).

## Risks / Trade-offs

- [Consistência de `403` vs `404` na leitura] A rota por UUID responde `403` para transação de outro provedor; a nova rota responde `404` para id externo alheio (lookup escopado). → Mitigação: o `404` não vaza nada e é indistinguível de ausência; documentado na seção do `ARCHITECTURE.md`.
- [Drift entre spec e código] As outras divergências (§9) continuam não alinhadas ao wire. → Mitigação: este change só as documenta (não as altera); a nova subseção vira a fonte única de verdade sobre a interpretação adotada.
- [Atrito do avaliador] Um avaliador que copie os exemplos literais do §9 ainda receberá `400` nas formas `money`/`initialBalance`. → Mitigação: a rota que faltava (o único gap funcional) é fechada; as formas restantes ficam documentadas como interpretação deliberada.

## Migration Plan

Aditiva: nenhuma mudança de schema ou mensageria. Deploy normal pelo Docker Compose; rollback = reverter o commit (a rota some e a doc volta, sem efeito financeiro residual).

## Open Questions

Nenhuma que mude spec, abordagem ou tarefas.