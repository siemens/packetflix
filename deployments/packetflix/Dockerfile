# Build our Packetflix capture-as-a-service Docker image in two stages,
# additionally trying to make good use of layer caching as possible.

# ⚠️ Requires Docker Buildx in order to correctly produce the final image with
# the extended file attributes for capabilities still intact. The Buildx plugin
# is automatically installed on recent .deb and .rpm based distributions.

# Nota bene: not all existing value combinations with respect to Alpine and Node
# versions might work, so you'll need to check Docker hub for the available base
# image versions.
ARG ALPINE_VERSION=3.18
ARG ALPINE_PATCH=4
ARG GO_VERSION=1.21.3

# 0th stage: https://github.com/tonistiigi/xx/blob/master/README.md
FROM --platform=${BUILDPLATFORM} tonistiigi/xx AS cc-helpers

# -- 1st stage ---------------------------------------------------------------
#
# Build our Packetflix application as a Go module. In addition, ensure that the
# Go app really gets compiled into a single static binary, without any DNS
# shared libs dependencies.
FROM --platform=${BUILDPLATFORM} golang:${GO_VERSION}-alpine${ALPINE_VERSION} AS builder

ARG BUILDPLATFORM
ARG TARGETPLATFORM

ARG WSDISKPATHS
ARG MOD1=./
ARG MOD2=./
ARG MOD3=./
ARG MOD4=./
ARG MOD5=./
ARG MOD6=./
ARG MOD7=./
ARG MOD8=./
ARG MOD9=./

# General warm-up
RUN apk add --no-cache git clang lld libcap-utils
COPY --from=cc-helpers / /
RUN xx-apk add --no-cache gcc musl-dev
# https://github.com/tonistiigi/xx/blob/master/README.md#go--cgo
ENV CGO_ENABLED=1
# As of Go 1.20 the Go distribution won't contain a pre-build std library
# anymore, so do this now and cache it.
RUN xx-go build std

WORKDIR /ws
# Copy the additionally used modules into the soon-to-be workspace.
COPY --from=bctx1 . ${MOD1}
COPY --from=bctx2 . ${MOD2}
COPY --from=bctx3 . ${MOD3}
COPY --from=bctx4 . ${MOD4}
COPY --from=bctx5 . ${MOD5}
COPY --from=bctx6 . ${MOD6}
COPY --from=bctx7 . ${MOD7}
COPY --from=bctx8 . ${MOD8}
COPY --from=bctx9 . ${MOD9}

# Make sure we have the main module containing a main package to be build...
COPY go.mod go.sum ./packetflix/

# Establish the Go workspace
RUN go work init ${WSDISKPATHS}

WORKDIR /ws/packetflix
# We now try to cache only the dependencies in a separate layer, so we can speed
# up things in case the dependencies do not change. This then reduces the amount
# of fetching and compiling required when compiling the final binary later.
RUN go mod download
# And now, finally, we build the packetflix service itself.
COPY *.go ./

ARG LDFLAGS="-s -w -extldflags=-static"
ARG TAGS="osusergo,netgo"

RUN --mount=target=. \
    --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg \
    xx-go build -v \
        -tags=$"{TAGS}" \
        -ldflags="${LDFLAGS}" \
        -o /packetflix . && \
    xx-verify --static /packetflix
RUN setcap "cap_sys_admin,cap_sys_chroot,cap_sys_ptrace,cap_net_admin,cap_net_raw+ep" /packetflix

# -- 2nd stage ---------------------------------------------------------------
#
# Build the final Docker image, containing only our Packetflix application and
# the required additional external tools, namely: nsenter and dumpcap.
FROM alpine:${ALPINE_VERSION}.${ALPINE_PATCH} as final
LABEL maintainer="Harald Albrecht <harald.albrecht@siemens.com>"
COPY --from=builder /packetflix /
# Now install the required dumpcap tool ... which brings in much more than we
# asked for. So we then immediately prune the installation, leaving only those
# things in that we really need. This saves us whooping: ~9.7M (of ~85M) on
# wireshark-common ... because libwireshark.so... is already ~75M :( Yet,
# tcpdump is only ~1M, but then doesn't support selectively capturing from a
# subset of network interfaces, but only from "all" which then bangs everthing
# into a single uniform capture mess.
RUN apk add --no-cache wireshark-common && \
    chmod a+x /usr/bin/dumpcap && \
    apk info -L wireshark-common | sed "1d;/^usr\/bin\/dumpcap/d;/^usr\/lib\/lib/d" | xargs rm -f && \
    rm -f /sbin/apk && rm -rf /etc/apk /lib/apk /usr/share/apk /var/lib/apk
EXPOSE 5000
USER 65534
ENTRYPOINT ["/packetflix", "--debug"]
