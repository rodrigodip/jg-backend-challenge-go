# Spec Delta

## Purpose

Garante que todo valor monetario da plataforma seja exato, auditavel e a prova de ponto flutuante, do parsing ao ledger.

## ADDED Requirements

### Requirement: Representacao exata sem ponto flutuante

O sistema SHALL representar valores monetarios em unidades minimas inteiras (`int64`) e NUNCA transportar dinheiro por `float32`/`float64` em parsing, calculo, serializacao ou persistencia.

#### Scenario: Parsing decimal exato

- **WHEN** uma entrada financeira traz `{"amount":"25.00","currency":"BRL"}`
- **THEN** o sistema interpreta exatamente 2500 unidades minimas de BRL sem perda

#### Scenario: Ponto flutuante proibido

- **WHEN** qualquer caminho processa dinheiro (HTTP, SQS, dominio, banco)
- **THEN** nenhum valor passa por tipo binario de ponto flutuante em nenhuma etapa

### Requirement: Contrato externo estrito de amount

O sistema SHALL aceitar `amount` somente como string com digitos, um ponto e exatamente duas casas (`^[0-9]+\.[0-9]{2}$`), e SHALL rejeitar vazio, sinal, espacos, `NaN`, `Infinity`, notacao cientifica, escala diferente de 2 e overflow de `int64`, sem normalizacao silenciosa e sem arredondamento.

#### Scenario: Formatos rejeitados

- **WHEN** chega `amount` como `"25"`, `"25.0"`, `"25.000"`, `" 25.00"`, `"-5.00"`, `"1e3"`, `""` ou numero JSON
- **THEN** o sistema rejeita como entrada corrigivel com `INVALID_MONEY`

#### Scenario: Sem normalizacao antes do hash

- **WHEN** duas representacoes textualmente diferentes denotam o mesmo numero (ex. `"25.00"` vs `"25.0"`)
- **THEN** o sistema as trata como entradas distintas para hash e a segunda e rejeitada por formato, nunca normalizada

### Requirement: Moeda ISO e compatibilidade

O sistema SHALL exigir codigo de moeda ISO 4217, carregar a moeda em todo valor, suportar BRL nos cenarios principais e USD para testes de incompatibilidade, e SHALL rejeitar aritmetica ou comparacao entre moedas diferentes e movimentacao com moeda divergente da carteira (`CURRENCY_MISMATCH`).

#### Scenario: Incompatibilidade entre moedas

- **WHEN** uma operacao em USD mira carteira BRL, ou se somam valores BRL + USD
- **THEN** o sistema rejeita com `CURRENCY_MISMATCH` e nenhum efeito financeiro ocorre

### Requirement: Politica de zero e negativos

O sistema SHALL exigir `amount > 0` para `BET`, `WIN`, `REFUND`, `ROLLBACK` e saldo inicial positivo quando houver abertura com valor; SHALL exigir literalmente `"0.00"` para `LOSS`; SHALL aceitar zero no saldo inicial e em `LOSS`; SHALL permitir negativos apenas em diferencas e calculos internos, nunca em saldo de carteira nem em entrada externa.

#### Scenario: Zero por tipo

- **WHEN** `BET` chega com `"0.00"` ou `LOSS` chega com `"10.00"`
- **THEN** o sistema rejeita com `INVALID_AMOUNT_FOR_KIND` sem persistir resultado

### Requirement: Overflow tratado e limites documentados

O sistema SHALL detectar overflow de `int64` em parsing, soma, subtracao e negacao (incluindo `MinInt64`), rejeitar a entrada ou falhar de forma segura sem corromper saldo, e SHALL documentar a representacao e seus limites (sem teto de negocio alem do `int64`).

#### Scenario: Soma que estoura int64

- **WHEN** um credito faria o saldo exceder o maximo representavel
- **THEN** o sistema nao aplica o movimento e registra falha permanente auditavel sem saldo negativo ou corrompido
