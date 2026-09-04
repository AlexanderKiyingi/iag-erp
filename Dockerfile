# syntax=docker/dockerfile:1
#
# Targets:
#   standalone (default) — iag-erp repo root on Railway
#   monorepo             — IAG_multi_backend root context (deploy/docker-compose)
#
# Monorepo:  docker build -f services/operations/erp/Dockerfile --target monorepo .
# Standalone: docker build -f Dockerfile --target standalone .

FROM golang:1.25-alpine AS base
RUN apk add --no-cache git ca-certificates
ENV PLATFORM_GO_DEP=/deps/platform-go

FROM base AS platform-go-copy
COPY shared/platform-go ${PLATFORM_GO_DEP}

FROM base AS build-standalone
# Standalone (iag-erp repo root): the meta-repo is private, so Railway can't
# clone it at build time. Instead the standalone repo carries a committed
# snapshot at third_party/platform-go (refreshed via
# scripts/sync-platform-go.sh). Copy that into /deps/platform-go and point the
# replace directive at it.
WORKDIR /src
COPY third_party/platform-go ${PLATFORM_GO_DEP}
COPY go.mod go.sum ./
RUN go mod edit -replace=github.com/alvor-technologies/iag-platform-go=${PLATFORM_GO_DEP} \
    && go mod download
COPY . .
# `COPY . .` restored go.mod from the build context, which still carries the
# meta-repo-only `replace => ../../../shared/platform-go`. That path does not
# exist inside the build container, so re-apply the vendored replace first.
RUN go mod edit -replace=github.com/alvor-technologies/iag-platform-go=${PLATFORM_GO_DEP} \
    && CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /erp ./cmd/server \
    && CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /erp-jobs ./cmd/erp-jobs \
    && CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /erp-migrate ./cmd/migrate

FROM base AS build-monorepo
COPY --from=platform-go-copy ${PLATFORM_GO_DEP} ${PLATFORM_GO_DEP}
WORKDIR /src/services/operations/erp
COPY services/operations/erp/go.mod services/operations/erp/go.sum ./
RUN go mod edit -replace=github.com/alvor-technologies/iag-platform-go=${PLATFORM_GO_DEP} \
    && go mod download
COPY services/operations/erp/ .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /erp ./cmd/server \
    && CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /erp-jobs ./cmd/erp-jobs \
    && CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /erp-migrate ./cmd/migrate

FROM alpine:3.21 AS monorepo
RUN apk add --no-cache ca-certificates tzdata wget
WORKDIR /app
COPY --from=build-monorepo /erp /app/erp
COPY --from=build-monorepo /erp-jobs /app/erp-jobs
# The schema migrator ships with the service it migrates.
#
# config.Validate refuses AUTO_MIGRATE=true in production, so migrations run out
# of band -- but `cmd/migrate` was never built into the image, so there was no
# out-of-band path from the running environment either. The only way to apply a
# migration was from somebody's laptop holding the production DATABASE_URL, and
# migrations sat in the repo looking deployed while never running (which is
# exactly what happened to 012).
#
# With this, an operator runs `/app/erp-migrate` as a one-off command against
# the service's own DATABASE_URL. It shares the runner, the embedded .sql files
# and the erp.schema_migrations ledger with the in-process path, takes the same
# advisory lock, and is safe to run while the service is up and safe to re-run.
COPY --from=build-monorepo /erp-migrate /app/erp-migrate
ENV PORT=4001 \
    GIN_MODE=release \
    AUTO_MIGRATE=false
EXPOSE 4001
HEALTHCHECK --interval=15s --timeout=5s --start-period=25s --retries=5 \
  CMD wget -q -O /dev/null http://127.0.0.1:4001/ready || exit 1
USER nobody
ENTRYPOINT ["/app/erp"]

FROM alpine:3.21 AS standalone
RUN apk add --no-cache ca-certificates tzdata wget
WORKDIR /app
COPY --from=build-standalone /erp /app/erp
COPY --from=build-standalone /erp-jobs /app/erp-jobs
# See the monorepo stage above: production cannot auto-migrate, so the migrator
# has to be reachable from the deployed environment.
COPY --from=build-standalone /erp-migrate /app/erp-migrate
ENV PORT=4001 \
    GIN_MODE=release \
    AUTO_MIGRATE=false
EXPOSE 4001
HEALTHCHECK --interval=15s --timeout=5s --start-period=25s --retries=5 \
  CMD wget -q -O /dev/null http://127.0.0.1:4001/ready || exit 1
USER nobody
ENTRYPOINT ["/app/erp"]
