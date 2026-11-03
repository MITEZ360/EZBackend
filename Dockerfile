# ============================================================================
# 三阶段构建：
#   1. web      —— Node 仅在构建期使用，产出静态文件
#   2. builder  —— 编译 Go 静态二进制，并把前端产物一并嵌入
#   3. runtime  —— 仅含二进制与证书，不含 Node
# ============================================================================
FROM node:22-alpine AS web

WORKDIR /web

RUN corepack enable
COPY web/package.json web/pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile

COPY web/ ./
RUN pnpm build

# ---------------------------------------------------------------------------
FROM golang:1.21-alpine AS builder

ARG VERSION=1.0.0
ARG COMMIT=dev
ARG BUILD_TIME=unknown

ENV GOPROXY=https://goproxy.cn,direct \
    GOSUMDB=off \
    CGO_ENABLED=0

WORKDIR /src
# 先复制依赖清单，利用镜像层缓存加速重复构建
COPY go.mod go.sum ./
RUN go mod download

COPY . .
COPY --from=web /web/dist ./web/dist

RUN go build -trimpath \
    -ldflags "-s -w \
      -X github.com/keel-framework/keel/pkg/version.Version=${VERSION} \
      -X github.com/keel-framework/keel/pkg/version.Commit=${COMMIT} \
      -X github.com/keel-framework/keel/pkg/version.BuildTime=${BUILD_TIME}" \
    -o /out/keel ./cmd/keel

# ---------------------------------------------------------------------------
FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata wget && \
    addgroup -g 10001 -S keel && adduser -u 10001 -S keel -G keel

ENV TZ=Asia/Shanghai

WORKDIR /app
COPY --from=builder /out/keel /app/keel
COPY configs/config.yaml /app/configs/config.yaml

RUN mkdir -p /app/data /app/logs /app/data/uploads && chown -R keel:keel /app
USER keel

EXPOSE 9000
VOLUME ["/app/data", "/app/logs"]

HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
    CMD wget -qO- http://127.0.0.1:9000/healthz || exit 1

ENTRYPOINT ["/app/keel"]
CMD ["-config", "/app/configs/config.yaml"]
