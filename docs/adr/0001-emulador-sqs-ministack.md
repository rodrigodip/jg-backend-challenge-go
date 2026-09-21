# ADR-0001: Emulador SQS — MiniStack

- Status: aceito
- IDs: D37 · Base: REQUISITOS.md §§4, 10, 15 · Spec: `messaging`

## Contexto

O REQUISITOS.md permite LocalStack ou MiniStack localmente e exige reprodução a
partir de checkout limpo sem segredos reais (§15).

## Decisão

**MiniStack** (`ministackorg/ministack:1.5.13`, tag fixada), porta 4566,
filas via hook `ready.d`. LocalStack descartado: desde 2026.03.0 exige conta
e auth token, o que quebra o checkout limpo.

## Consequências

- Divergências comprovadas por `tests/sqs_compat_test.go` (tag `integration`):
  release-to-0 com receipt handle obsoleto é ignorado; queue policies são
  armazenadas mas **não aplicadas** — validações de domínio no consumidor são
  obrigatórias (§2). A correção financeira nunca depende do broker (§5.3).
- Chaves de acesso não numéricas por papel caem na conta padrão e enxergam as
  mesmas filas (chave de 12 dígitos vira ID de conta e isola).
