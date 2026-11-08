FROM --platform=$BUILDPLATFORM node:22-bookworm-slim AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN --mount=type=cache,target=/root/.npm npm ci
COPY web/ ./
COPY internal/i18n/messages/ /src/internal/i18n/messages/
RUN npm run build

FROM --platform=$BUILDPLATFORM golang:1.27-bookworm AS build
ARG TARGETARCH
ARG ZIG=0.16.0
ARG SPIDERMONKEY=v152.0.4-aistudio2api.1
ARG LIBWEBP=v1.6.0-aistudio2api.1
RUN apt-get update && apt-get install -y --no-install-recommends xz-utils && rm -rf /var/lib/apt/lists/*
COPY build/dependency-checksums.txt build/fetch-verified.sh /opt/verify/
# 摘要来源写在 dependency-checksums.txt。校验失败不会解压。
RUN arch=$(uname -m) \
    && sh /opt/verify/fetch-verified.sh "https://ziglang.org/download/${ZIG}/zig-${arch}-linux-${ZIG}.tar.xz" "zig-${arch}-linux-${ZIG}.tar.xz" /opt /opt/verify/dependency-checksums.txt tar.xz \
    && mv /opt/zig-* /opt/zig \
    && mkdir -p /opt/spidermonkey /opt/libwebp \
    && sh /opt/verify/fetch-verified.sh "https://github.com/Mag1cFall/firefox/releases/download/${SPIDERMONKEY}/spidermonkey-${SPIDERMONKEY}-linux-${TARGETARCH}.tar.gz" "spidermonkey-${SPIDERMONKEY}-linux-${TARGETARCH}.tar.gz" /opt/spidermonkey /opt/verify/dependency-checksums.txt tar.gz \
    && sh /opt/verify/fetch-verified.sh "https://github.com/Mag1cFall/libwebp/releases/download/${LIBWEBP}/libwebp-${LIBWEBP}-linux-${TARGETARCH}.tar.gz" "libwebp-${LIBWEBP}-linux-${TARGETARCH}.tar.gz" /opt/libwebp /opt/verify/dependency-checksums.txt tar.gz
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
COPY --from=web /src/internal/webui/dist internal/webui/dist
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    target=$([ "$TARGETARCH" = arm64 ] && echo aarch64 || echo x86_64) \
    && CGO_ENABLED=1 GOOS=linux GOARCH=$TARGETARCH \
    CC="/opt/zig/zig cc -target $target-linux-musl" CXX="/opt/zig/zig c++ -target $target-linux-musl" \
    CGO_CPPFLAGS="-I/opt/spidermonkey/include -I/opt/libwebp/include" CGO_LDFLAGS="-L/opt/spidermonkey/lib -L/opt/libwebp/lib" \
    go build -trimpath -tags netgo,osusergo -ldflags="-s -w -linkmode external -extldflags '-static -Wl,--strip-all'" -o /out/aistudio2api ./cmd/aistudio2api

FROM debian:bookworm-slim AS runtime
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates tzdata \
    libgtk-3-0 libasound2 libnss3 libdbus-glib-1-2 libxtst6 libxrandr2 libgbm1 libxkbcommon0 \
    libpango-1.0-0 libcairo2 libxcomposite1 libxdamage1 libxfixes3 libx11-xcb1 \
    xvfb \
    && rm -rf /var/lib/apt/lists/* \
    && groupadd -r app && useradd -r -g app app \
    && mkdir -p /data && chown app:app /data
WORKDIR /data
USER app
ENV LISTEN_ADDR=0.0.0.0:2048
ENV PREVIEW_LISTEN_ADDR=0.0.0.0:2050
EXPOSE 2048 2050
CMD ["aistudio2api", "-open-ui=false"]

FROM runtime AS release
ARG TARGETARCH
COPY release/linux-$TARGETARCH/aistudio2api /usr/local/bin/aistudio2api

FROM runtime
COPY --from=build /out/aistudio2api /usr/local/bin/aistudio2api
