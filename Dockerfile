# Build go
# Must match the toolchain in go.mod (toolchain go1.25.2). The official golang
# images set GOTOOLCHAIN=local, so an older base image makes `go mod download`
# fail with "go.mod requires go >= 1.25".
#
# The builder runs on the build platform and cross compiles for the target one.
# With CGO disabled Go does not need a target toolchain or an emulator, and the
# arm64 half of the image stops being built under QEMU: that step took 39 minutes
# on the release runner, the cross build takes about a minute.
FROM --platform=$BUILDPLATFORM golang:1.25.2-alpine AS builder
ARG TARGETOS
ARG TARGETARCH
WORKDIR /app
ENV CGO_ENABLED=0
# The module files come first so editing a source file does not invalidate the
# download layer.
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download
COPY . .
# -s -w -buildid= and -trimpath match what the release binaries are built
# with: the debug info is most of the 200MB of the unstripped binary, and the
# image keeps only the binary.
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    GOOS=$TARGETOS GOARCH=$TARGETARCH go build -v -o V2bX -trimpath \
    -ldflags "-s -w -buildid=" \
    -tags "sing xray hysteria2 with_reality_server with_quic with_grpc with_utls with_wireguard with_acme with_gvisor"

# Release
FROM  alpine
# 安装必要的工具包
RUN  apk --update --no-cache add tzdata ca-certificates \
    && cp /usr/share/zoneinfo/Asia/Shanghai /etc/localtime
RUN mkdir /etc/V2bX/
COPY --from=builder /app/V2bX /usr/local/bin

ENTRYPOINT [ "V2bX", "server", "--config", "/etc/V2bX/config.json"]
