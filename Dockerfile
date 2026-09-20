# syntax=docker/dockerfile:1
ARG GO_IMAGE=golang:1.26-alpine3.22

FROM ${GO_IMAGE} AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /out/app ./cmd/app \
 && CGO_ENABLED=0 go build -trimpath -o /out/migrate ./cmd/migrate

FROM alpine:3.22
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=builder /out/app /out/migrate ./
COPY migrations ./migrations
EXPOSE 8080 9090
# CMD (not ENTRYPOINT) so Compose can replace the binary per role:
# api/consumer/workers run /app/app, the migrate job runs /app/migrate.
CMD ["/app/app"]
