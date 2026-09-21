# ADR-0007: Idempotência financeira + transporte

- Status: aceito
- IDs: D18(revisto), D19, D20, D39, D45 · Base: REQUISITOS.md §§5, 9, 10 · Specs: `idempotency-contracts`, `messaging`

## Contexto

Idempotência persistente (sobrevive a restart total), replay cross HTTP×SQS
com saldo original, conflito detectável; chave nunca substituída
silenciosamente. O pré-processamento exigia "sem espaços" na chave —
restrição extra fora do contrato.

## Decisão

Chave opaca, obrigatória, não-vazia, ≤255 (sem restrição de espaços).
SHA-256/RFC8785 dos 10 campos de negócio (chave e transporte fora).
Duplo unique `(provider, chave)` + `(provider, externalId)` com handler:
hash igual → replay `200` com saldo original; diferente → `409
IDEMPOTENCY_CONFLICT`. Corrigíveis não persistem nem reservam chave.
Transporte: inbox `(consumer, messageId)` com hash; mesmo `messageId` com
hash divergente → DLQ. `MessageDeduplicationId` = `messageId` na entrada;
testes de duplicidade usam dedups distintos (§13). Correlação: header
`X-Correlation-Id` ou gerado; SQS deriva do `messageId`.

## Consequências

- Replay nunca reaplica movimento; 50× paralelo resulta em 1 débito.
- Mapear violação de unique direto para 409 quebraria replay legítimo.
