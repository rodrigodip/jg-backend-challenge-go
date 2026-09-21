# ADR-0002: Binário único com modos Fx

- Status: aceito
- IDs: D02, D05, D06 · Base: REQUISITOS.md §4 · Spec: `auth-observability`

## Contexto

O desafio exige Uber Fx com `Module/Provide/Invoke`, lifecycle com shutdown
gracioso e domínio independente de infraestrutura, demonstrável em 3+
processos independentes.

## Decisão

Binário único com `-mode api|consumer|workers`; cada modo é um `fx.Module`.
Outbox e referências rodam no modo `workers` (escalável), nunca embutidos só
no `api`. Config por env, validada no start (falha rápida). Domínio puro,
sem Fx/HTTP/SQS/GORM.

## Consequências

- Réplicas independentes por papel no Compose e nos testes (§§8, 13.4).
- Shutdown: para novas entradas, conclui ou libera o voo, fecha dependências
  após os consumidores.
