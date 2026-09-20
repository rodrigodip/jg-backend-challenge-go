# ADR-0000: Usar ADRs como log de decisões

- Status: aceito
- Data: 2026-09-20

## Contexto

O pré-processamento (`decisoes-desafio-backend.md`) acumulou decisões nos
formatos de blocos 1–8, mas virou fonte paralela à change OpenSpec e ficou
obsoleto em 5 pontos (teto de 1M, chave sem espaços, TTL 15s, 422 duplo,
falta do `BET_HAS_ACTIVE_WIN`), mantendo o cabeçalho "nenhuma decisão pode
contradizê-lo".

## Decisão

Registrar decisões em ADRs sob `docs/adr/` (MADR enxuto, em PT), um por
cluster, com links para `README.md §`, specs e IDs `Dxx/Kxx` originais.
`ARCHITECTURE.md` segue como síntese exigida pelo §15; specs seguem como
contratos testáveis.

## Consequências

- Uma só fonte por camada: README (desafio) → ADRs (porquê) → specs (o quê).
- O arquivo de pré-processamento é removido no mesmo commit; git preserva o histórico.
- `tasks.md` 7.1 da change de fundação passa a rastrear ADRs.
