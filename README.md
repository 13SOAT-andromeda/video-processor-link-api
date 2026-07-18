# links-service

Serviço de Links do Tech Challenge FIAP X (fase 5), conforme a spec `arquitetura-video-processing-tech-challenge.md`. Fonte única da verdade do domínio de links (ADR-007): API HTTP (Gin), máquina de estados, persistência em DynamoDB (`Links`/`LinkEvents` com TTL nativo), presigned URLs do S3, consumer contínuo da `video-processing-status-queue` e publicação no `notification-topic` em falha de processamento (ADR-008).

O **users-api não está no ar ainda** — a resolução de e-mail/nome usa um mock (`USE_USER_SVC_MOCK=true`, padrão). Quando a svc ficar pronta, basta `USE_USER_SVC_MOCK=false` + `USERS_BASE_URL` (o client HTTP do contrato real `GET /users/:id` já está implementado — a antiga rota interna `/internal/users/:id` foi eliminada pela plataforma no ADR-012; o client assina um service token HS256 com o mesmo segredo compartilhado `jwt-signing-key`).

O **authorizer** (Lambda) também está fora do escopo — um middleware JWT (HS256) simula o comportamento: valida o token e injeta `userId`/`role`.

## Rodando localmente (WSL)

Pré-requisitos: Go 1.22+, Docker (para o LocalStack).

```bash
cp .env.example .env
make deps        # go mod tidy
make infra-up    # LocalStack + tabelas/fila/bucket/tópico
make test
make run
```

## Testando o fluxo

```bash
TOKEN=$(make -s token)          # JWT de user (u-123)
ADMIN=$(make -s token-admin)    # JWT de administrator

# 1. criar link -> devolve linkId + presigned PUT
curl -s -X POST localhost:8080/links \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"fileName":"video.mp4","fileSize":1048576,"isPrivate":false}'

# 2. subir o "vídeo" na presigned URL (uploadUrl do passo 1)
curl -X PUT "<uploadUrl>" --data-binary @algum-arquivo.mp4

# 3. callback pós-upload -> UPLOAD_COMPLETED -> PROCESSING_PENDING
curl -s -X PUT localhost:8080/links/<linkId>/upload -H "Authorization: Bearer $TOKEN"

# 4. simular o processing-worker publicando na status-queue
make simulate-worker LINK=<linkId> EVENT=PROCESSING_STARTED
make simulate-worker LINK=<linkId> EVENT=PROCESSING_COMPLETED KEY='<linkId>/processed/video.zip'
#    (ou falha, que dispara a notificação SNS com usuário mockado)
make simulate-worker LINK=<linkId> EVENT=PROCESSING_FAILED REASON=max_retries_exceeded

# 5. consultar
curl -s localhost:8080/links/<linkId>        -H "Authorization: Bearer $TOKEN"
curl -s localhost:8080/links/<linkId>/events -H "Authorization: Bearer $TOKEN"
curl -s localhost:8080/links/<linkId>/download -H "Authorization: Bearer $TOKEN"
curl -s localhost:8080/links                 -H "Authorization: Bearer $ADMIN"
```

## Endpoints (spec §5)

| Método | Rota | Roles |
|---|---|---|
| POST | `/links` | user, administrator |
| GET | `/links` | administrator |
| GET | `/links/user/:id` | dono ou administrator |
| GET | `/links/:id` | dono ou administrator |
| GET | `/links/:id/events` | dono ou administrator |
| PUT | `/links/:id/upload` | dono ou administrator |
| GET | `/links/:id/download` | dono ou administrator (exige `PROCESSING_COMPLETED`) |

Erros: `400 INVALID_FILE_SIZE`, `401 UNAUTHORIZED`, `403 FORBIDDEN`, `404 LINK_NOT_FOUND`, `409 INVALID_STATUS_TRANSITION`.

## Máquina de estados

```
LINK_CREATED ─┬─> UPLOAD_PENDING ──> UPLOAD_COMPLETED ─┬─> PROCESSING_PENDING ─┐
              ├─────────────────────────^              └──────────┐            │
              └─> UPLOAD_FAILED (terminal)                        v            v
                                                        PROCESSING_STARTED ─┬─> PROCESSING_COMPLETED (terminal)
                                                        (FAILED de qualquer └─> PROCESSING_FAILED (terminal)
                                                         estado de processing)
```

Transições aplicadas exclusivamente pelo `links-service` (ADR-007), com optimistic locking no DynamoDB (condição sobre o status esperado) e idempotência para a entrega at-least-once do SQS.

## Estrutura

```
cmd/api            main (wire de dependências, HTTP + consumer goroutine)
cmd/token          gerador de JWT de dev (simula a Lambda authentication)
internal/domain/link   entidades + máquina de estados (zero dependência de infra)
internal/app           casos de uso + portas (interfaces)
internal/adapters/
  httpapi          Gin handlers + middleware JWT (simula o authorizer)
  dynamo           repositório Links/LinkEvents
  storage          presigned PUT/GET do S3
  queue            consumer SQS (long polling)
  notification     publisher SNS (contrato ADR-008) + noop
  users            mock do users-service + client HTTP do contrato real
deploy/localstack  docker-compose + bootstrap de recursos
```
