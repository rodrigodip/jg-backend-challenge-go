# ADR-0009: Outbox transacional best-effort

- Status: aceito
- IDs: D40a, D42–D44 · Base: REQUISITOS.md §§5, 11 · Spec: `messaging`

## Contexto

Eventos só após o commit de origem; publishers múltiplos com disputa;
recuperação entre commit↔publicação e publicação↔confirmação; republicação
preserva `eventId`. Ordem global estrita exigiria lock global (proibido §5.6).

## Decisão

Estado+saldo+ledger+inbox+eventos no mesmo commit; worker separado com claim
`SKIP LOCKED` + lease e backoff; ordem best-effort, consumidor deduplica por
`eventId` e ordena por `walletVersion` (eventos sem bump carregam a versão
corrente). `MessageGroupId = walletId`, dedup = `eventId`,
`aggregateId = walletId`. Eventos: `Processed` (inclui `LOSS`),
`Rejected`, `BalanceChanged` (só em mudança efetiva), `PendingReference`;
nada para `FAILED`. Inválida/permanente → DLQ explícita; `REJECTED`
definitivo remove após commit; transitória retenta via visibility.

## Consequências

- Abertura positiva grava carteira + `OPENING` + ledger + 2 eventos na mesma tx.
- `SIGTERM`: conclui em 30s ou libera visibilidade (handles sempre correntes,
  dada a divergência do emulador com handles obsoletos).
