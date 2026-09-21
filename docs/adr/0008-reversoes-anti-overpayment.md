# ADR-0008: Reversões seamless anti-overpayment

- Status: aceito
- IDs: D24, D24b, D26, D28 · Base: REQUISITOS.md §7 · Spec: `wagering`

## Contexto

O REQUISITOS.md exige documentar combinações REFUND×ROLLBACK sobre a mesma aposta e
impedir devolução duplicada; não define o caso clássico de overpayment
(stake de volta + prêmio). Padrão seamless de igaming: refund só de BET,
rollback como void.

## Decisão

`REFUND` só de `BET` processada; `ROLLBACK` de `BET`/`WIN`/`REFUND`;
nunca de `LOSS`/`OPENING`/não-processada, nunca `ROLLBACK` de `ROLLBACK`;
uma reversão direta por `BET` (`ALREADY_REVERSED`); **`BET` com `WIN`
`PROCESSED` dependente não revertido bloqueia (`BET_HAS_ACTIVE_WIN`)**;
`ROLLBACK` de `REFUND` debita (void) e libera a `BET`. Valores integrais,
concordância em provedor/jogador/carteira/moeda/rodada; reversão que
quebraria o saldo → `ROLLBACK_INSUFFICIENT_BALANCE` (distinto de aposta sem
saldo). `WIN` com referência opcional validada (BET processada, mesma
rodada/carteira/moeda) ou `PENDING_REFERENCE`.

## Consequências

- `REFUND` após `WIN` ativo é negado em vez de criar dinheiro.
- Cadeia suportada `BET→REFUND→ROLLBACK-do-REFUND` documentada com diagrama.
