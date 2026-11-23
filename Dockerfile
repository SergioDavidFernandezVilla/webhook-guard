# syntax=docker/dockerfile:1

FROM golang:1.24-alpine AS build

WORKDIR /src

RUN apk add --no-cache ca-certificates

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY . .

ARG TAGS=""

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -tags "${TAGS}" -o /out/webhook-guard ./cmd/webhook-guard \
 && CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -tags "${TAGS}" -o /out/loadgen ./cmd/loadgen

FROM alpine:3.21 AS runtime

RUN apk add --no-cache ca-certificates tzdata \
 && addgroup -S app \
 && adduser -S -G app -h /app app

WORKDIR /app

COPY --from=build /out/webhook-guard /usr/local/bin/webhook-guard
COPY --from=build /out/loadgen /usr/local/bin/loadgen
COPY --chown=app:app migrations ./migrations

USER app

EXPOSE 8080

ENTRYPOINT ["webhook-guard"]
CMD ["serve"]
