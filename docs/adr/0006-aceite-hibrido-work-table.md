# ADR-0006: Aceite híbrido com work-table

- Status: aceito
- IDs: D17, D17b, D25, D27(revisto), D29c · Base: README §§6.3, 7 · Spec: `wagering`

## Contexto

Todo `PENDING` confirmado precisa de retomada por outra instância; operações
sem dependência podem concluir síncronas sem commit intermediário; referência
ausente vira `PENDING_REFERENCE` com TTL/tentativas e evento, nunca descarte
silencioso.

## Decisão

Aceite híbrido: mesma tx grava aceite + `work_items` e tenta inline;
concluindo responde `201`/`422`, senão `202` e o worker assume (lease +
`SKIP LOCKED`). Work-table separada da outbox (`kind`, contadores ref/infra
separados: 5 ref + 10 infra, `next_attempt_at`, lease). **TTL 60s** (não 15s:
15s morre em teste manual), backoff full-jitter 200ms→3s, reavaliação imediata
na tx da referência + polling 500ms. Esgotado → `REJECTED REFERENCE_NOT_FOUND`
com evento; referência terminal sem sucesso → `REFERENCE_NOT_PROCESSED`.

## Consequências

- §13.8 (kill após `PENDING`) atendido por construção.
- Wake-up via broker descartado: não atravessa réplicas; correção vem do claim.
