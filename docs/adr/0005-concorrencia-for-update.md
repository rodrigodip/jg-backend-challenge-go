# ADR-0005: Concorrência por carteira com `FOR UPDATE`

- Status: aceito
- IDs: D16, D30 · Base: REQUISITOS.md §§5, 8 · Spec: `wallet-ledger`

## Contexto

Locks globais são proibidos; carteiras independentes avançam em paralelo;
lost updates são proibidos; o teste 100/80/80 exige serialização por carteira
entre processos sem memória compartilhada.

## Decisão

Lock pessimista por linha (`SELECT … FOR UPDATE` na carteira), sem lock
global. `version` inicia em 1 e só incrementa em mudança de saldo (é a
sequência do cursor do ledger). Sem vínculo provedor↔carteira: a wallet é do
operador/jogador e provedores distintos a compartilham (seamless); o vínculo
`playerId` é validado em toda operação.

## Consequências

- Referência e operação concordam em wallet (§7) → mesmo lock, sem deadlock
  na cascata de reavaliação.
- Alternativas (otimista com retry) exigiriam retry até em `REJECTED` por saldo.
