# Tasks

## 1. ADRs

- [x] 1.1 Escrever `docs/adr/README.md` (índice, convenção MADR, statuses) e `docs/adr/0000-usar-adrs.md` e verificar ambos linkam os 10 ADRs numerados
- [x] 1.2 Escrever ADRs 0001–0005 (emulador, modos Fx, GORM contido, money int64 sem teto, concorrência FOR UPDATE) com links README §, specs e D-ids e verificar `grep` encontra cada D-id citado
- [x] 1.3 Escrever ADRs 0006–0010 (aceite híbrido, idempotência, reversões anti-overpayment, outbox, Keycloak) com as 5 divergências do arquivo obsoleto corrigidas e verificar nenhum valor obsoleto (1M, 15s, 422-duplo) permanece

## 2. Corte e rastreabilidade

- [x] 2.1 Deletar `decisoes-desafio-backend.md` e ajustar `tasks.md` 7.1 da change de fundação para rastrear ADRs e verificar `grep -ri decisoes-desafio-backend` retorna vazio fora do git history
- [x] 2.2 Validar a change (`openspec validate`) e verificar `go build ./...` continua verde após a remoção
