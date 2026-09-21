# ADR-0004: Money em `int64`, sem teto de negócio

- Status: aceito
- IDs: D10, D11(revisto), D21, D23 · Base: REQUISITOS.md §6.1 · Spec: `money`

## Contexto

O REQUISITOS.md proíbe `float32/64` em todo o caminho, exige escala fixa de 2 casas
e rejeição sem arredondamento silencioso, mas não impõe teto de valor. O
pré-processamento impunha 1M por entrada — restrição extra que reprovaria
high-rollers e testes com valores altos.

## Decisão

`int64` em unidades mínimas; `amount` só como string `^[0-9]+\.[0-9]{2}$`
(sem normalização antes do hash; `LOSS` exige `"0.00"`); overflow tratado em
parse, soma, subtração e negação (inclui `MinInt64`); **sem teto além do
`int64`**, documentado como limite. BRL principal + USD para incompatibilidade.

## Consequências

- Compatibilidade literal com o contrato `{"amount":"25.00","currency":"BRL"}`.
- Abuso de valor passa a ser tema de produto (config futura), não de correção.
