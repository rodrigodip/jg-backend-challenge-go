# Proposal

## Why

`decisoes-desafio-backend.md` declara ser autoridade ("nenhuma decisão pode contradizê-lo") mas está obsoleto em 5 pontos frente às decisões consolidadas na change de fundação, o que induz o avaliador a erro. ADRs em `docs/adr/` viram o log auditável padrão de mercado.

## What Changes

- Cria `docs/adr/` com índice e 11 ADRs (MADR enxuto, em PT, todos `aceito`), cada um com links para `README.md §`, specs e IDs `Dxx/Kxx` preservados.
- **BREAKING** Remove `decisoes-desafio-backend.md` no mesmo commit dos ADRs (histórico preservado no git).
- Ajusta `tasks.md` 7.1 da change de fundação para rastrear ADRs em vez do arquivo removido.

## Capabilities

### New Capabilities

(nenhuma — mudança exclusiva de documentação)

### Modified Capabilities

(nenhuma — nenhum comportamento muda)

## Impact

- Docs: `docs/adr/*`, remoção de um arquivo, uma linha em `tasks.md` de outra change.
- Zero impacto em código, migrations, Compose ou contratos.
