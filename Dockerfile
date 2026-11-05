FROM --platform=$BUILDPLATFORM golang:1.27-bookworm AS build
ARG TARGETOS
ARG TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w" -o /out/z-lottery ./cmd/z-lottery

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates \
    && rm -rf /var/lib/apt/lists/* \
    && groupadd --gid 1000 zlottery \
    && useradd --uid 1000 --gid 1000 --no-create-home zlottery \
    && mkdir -p /app/data && chown 1000:1000 /app/data && chmod 700 /app/data
ENV HOST=0.0.0.0 \
    PORT=3000 \
    DATABASE_PATH=/app/data/lottery.sqlite \
    CHECK_ENABLED=true \
    CHECK_SCHEDULE="*/20 22-23 * * *;0 0 * * *" \
    CHECK_TIMEZONE=Asia/Shanghai
WORKDIR /app
COPY --from=build /out/z-lottery /app/z-lottery
USER 1000:1000
EXPOSE 3000
HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 CMD ["/app/z-lottery", "healthcheck"]
CMD ["/app/z-lottery"]
