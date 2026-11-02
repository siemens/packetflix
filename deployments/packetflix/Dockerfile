# Build our Packetflix capture-as-a-service Docker image in two stages,
# additionally trying to make good use of layer caching as possible.

# ⚠️ Requires Docker Buildx in order to correctly produce the final image with
# the extended file attributes for capabilities still intact. The Buildx plugin
# is automatically installed on recent .deb and .rpm based distributions.

# Nota bene: not all existing value combinations with respect to Alpine and Go
# versions might work, so you'll need to check Docker hub for the available base
# image versions.
ARG ALPINE_VERSION=3.23
ARG GO_VERSION=1.26

# 0th stage: https://github.com/tonistiigi/xx/blob/master/README.md
FROM --platform=${BUILDPLATFORM} tonistiigi/xx:1.10.0@sha256:40fe21f44bb8cddf1eb1226db190d0a984118f62ee02a91b77d99448d28264a6 AS cc-helpers

# -- 1st stage ---------------------------------------------------------------
#
# Build our Packetflix application as a Go module. In addition, ensure that the
# Go app really gets compiled into a single static binary, without any DNS
# shared libs dependencies.
FROM --platform=${BUILDPLATFORM} golang:${GO_VERSION}-alpine${ALPINE_VERSION} AS builder

ARG TARGETPLATFORM

# General warm-up
RUN apk add --no-cache clang lld libcap-utils
COPY --from=cc-helpers / /
RUN xx-apk add --no-cache gcc musl-dev
# https://github.com/tonistiigi/xx/blob/master/README.md#go--cgo
ENV CGO_ENABLED=1
# As of Go 1.20 the Go distribution won't contain a pre-build std library
# anymore, so do this now and cache it.
RUN --mount=type=cache,target=/root/.cache/go-build \
    xx-go build std

WORKDIR /src
# We now try to cache only the dependencies, so we can speed up things in case
# the dependencies do not change.
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg \
    go mod download

ARG LDFLAGS="-s -w -extldflags=-static"
ARG TAGS="osusergo,netgo"
# The build context lacks .git, so the version needs to be passed in, such as
# from "git describe --tags".
ARG VERSION=""

RUN --mount=target=. \
    --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg \
    xx-go build -v \
        -tags="${TAGS}" \
        -ldflags="${LDFLAGS} -X main.version=${VERSION}" \
        -o /packetflix ./cmd/packetflix && \
    xx-verify --static /packetflix
# Containers must grant (at least) all these capabilities, otherwise exec fails
# with "operation not permitted".
RUN setcap "cap_sys_admin,cap_sys_chroot,cap_sys_ptrace,cap_dac_read_search,cap_net_admin,cap_net_raw+ep" /packetflix

# -- 2nd stage ---------------------------------------------------------------
#
# Build the final Docker image, containing only our Packetflix application and
# the required additional external tool, namely: dumpcap.
FROM alpine:${ALPINE_VERSION} AS final
WORKDIR /
COPY --from=builder /packetflix /
# Now install the required dumpcap tool ... which brings in much more than we
# asked for. So we then immediately prune the installation, leaving only those
# things in that we really need. This saves us whooping: ~9.7M (of ~85M) on
# wireshark-common ... because libwireshark.so... is already ~75M :( Yet,
# tcpdump is only ~1M, but then doesn't support selectively capturing from a
# subset of network interfaces, but only from "all" which then bangs everthing
# into a single uniform capture mess.
# Alpine's dumpcap carries file capabilities; drop them, as packetflix passes
# them on as ambient capabilities only to its own dumpcap child processes.
RUN apk add --no-cache wireshark-common libcap-utils && \
    setcap -r /usr/bin/dumpcap && \
    apk del --no-cache libcap-utils && \
    chmod a+x /usr/bin/dumpcap && \
    apk info -L wireshark-common | sed "1d;/^usr\/bin\/dumpcap/d;/^usr\/lib\/lib/d" | xargs rm -f && \
    rm -f /sbin/apk && rm -rf /etc/apk /lib/apk /usr/share/apk /var/lib/apk
EXPOSE 5001
USER 65534
ENTRYPOINT ["/packetflix", "--debug"]
# Labels last, so that a changing VERSION doesn't invalidate the layers above.
ARG VERSION=""
LABEL org.opencontainers.image.title="packetflix" \
      org.opencontainers.image.description="Virtual network packet capture streaming service" \
      org.opencontainers.image.source="https://github.com/siemens/packetflix" \
      org.opencontainers.image.licenses="MIT" \
      org.opencontainers.image.authors="Harald Albrecht <harald.albrecht@siemens.com>" \
      org.opencontainers.image.version="${VERSION}"
