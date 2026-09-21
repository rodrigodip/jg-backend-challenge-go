# Architecture Decision Records

Log auditável das decisões de arquitetura do desafio. Cada ADR registra
contexto, decisão e consequências; o `ARCHITECTURE.md` (exigido pelo
REQUISITOS.md §15) é a síntese narrativa, e as specs em
`openspec/specs/` são os contratos testáveis. Em caso de divergência,
vale o `REQUISITOS.md`.

Convenção: MADR enxuto em PT. Status possíveis: `aceito`, `proposto`,
`substituído`. IDs `Dxx/Kxx` preservam a rastreabilidade com o
pré-processamento original (removido; histórico no git).

## Índice

| ADR | Título | Status |
|-----|--------|--------|
| [0000](0000-usar-adrs.md) | Usar ADRs como log de decisões | aceito |
| [0001](0001-emulador-sqs-ministack.md) | Emulador SQS: MiniStack | aceito |
| [0002](0002-modos-fx-binario-unico.md) | Binário único com modos Fx | aceito |
| [0003](0003-persistencia-gorm-contido.md) | Persistência: GORM contido | aceito |
| [0004](0004-money-int64-sem-teto.md) | Money em `int64`, sem teto de negócio | aceito |
| [0005](0005-concorrencia-for-update.md) | Concorrência por carteira com `FOR UPDATE` | aceito |
| [0006](0006-aceite-hibrido-work-table.md) | Aceite híbrido com work-table | aceito |
| [0007](0007-idempotencia-dupla-unicidade.md) | Idempotência financeira + transporte | aceito |
| [0008](0008-reversoes-anti-overpayment.md) | Reversões seamless anti-overpayment | aceito |
| [0009](0009-outbox-best-effort.md) | Outbox transacional best-effort | aceito |
| [0010](0010-auth-keycloak-client-credentials.md) | Auth OIDC com Keycloak | aceito |
