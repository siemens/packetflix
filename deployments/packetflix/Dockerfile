# Build our Packetflix capture-as-a-service Docker image as a distroless-like
# image: a static Packetflix binary plus dumpcap with only the shared libraries
# it needs, unpacked from a fixed Debian stable snapshot, without any shell or
# package manager. Additionally, we try to make good use of layer caching.

# Debian packages come from snapshot.debian.org as of this point in time, so
# rebuilds get the same package versions; bump it to pick up (security) updates.
ARG DEBIAN_SNAPSHOT=20261101T000000Z

# https://github.com/tonistiigi/xx/blob/master/README.md
FROM --platform=${BUILDPLATFORM} tonistiigi/xx:1.10.0@sha256:40fe21f44bb8cddf1eb1226db190d0a984118f62ee02a91b77d99448d28264a6 AS cc-helpers

# -- Debian base -------------------------------------------------------------
#
# Debian stable with the Go toolchain, and its package sources pointing to our
# fixed snapshot.
FROM --platform=${BUILDPLATFORM} golang:1.26-trixie@sha256:a050dcceee6eb9bf110151662dfac7662f3bec40927c7dee48731e3d92083d42 AS debian

ARG DEBIAN_SNAPSHOT

SHELL ["/bin/bash", "-o", "pipefail", "-ec"]
COPY --from=cc-helpers / /
# Plain http is fine, as apt checks the signed Release files; these will have
# expired for older snapshots, though.
RUN sed -i -E "s#^URIs: http://deb.debian.org/(debian(-security)?)\$#URIs: http://snapshot.debian.org/archive/\1/${DEBIAN_SNAPSHOT}#" \
        /etc/apt/sources.list.d/debian.sources && \
    echo 'Acquire::Check-Valid-Until "false";' > /etc/apt/apt.conf.d/99snapshot

# -- 1st stage ---------------------------------------------------------------
#
# Build our Packetflix application as a Go module. In addition, ensure that the
# Go app really gets compiled into a single static binary, without any DNS
# shared libs dependencies.
FROM debian AS builder

ARG TARGETPLATFORM

# General warm-up
RUN apt-get update && \
    apt-get install -y --no-install-recommends clang lld libcap2-bin && \
    xx-apt-get install -y --no-install-recommends gcc libc6-dev
# https://github.com/tonistiigi/xx/blob/master/README.md#go--cgo
# cgo is required by lxkns' mntnssandbox (C constructor does setns before the Go runtime starts).
ENV CGO_ENABLED=1
# As of Go 1.20 the Go distribution won't contain a pre-build std library
# anymore, so do this now and cache it.
RUN --mount=type=cache,target=/root/.cache/go-build \
    xx-go build std

WORKDIR /src
# We now try to cache only the dependencies, so we can speed up things in case
# the dependencies do not change. No cache mount, so that the modules end up in
# the layer and thus also in the CI build cache.
COPY go.mod go.sum ./
RUN go mod download

ARG LDFLAGS="-s -w -extldflags=-static"
ARG TAGS="osusergo,netgo"
# The build context lacks .git, so the version needs to be passed in, such as
# from "git describe --tags".
ARG VERSION=""

RUN --mount=target=. \
    --mount=type=cache,target=/root/.cache/go-build \
    xx-go build -v \
        -trimpath \
        -tags="${TAGS}" \
        -ldflags="${LDFLAGS} -X main.version=${VERSION}" \
        -o /packetflix ./cmd/packetflix && \
    xx-verify --static /packetflix
# Containers must grant (at least) all these capabilities, otherwise exec fails
# with "operation not permitted".
RUN setcap "cap_sys_admin,cap_sys_chroot,cap_sys_ptrace,cap_dac_read_search,cap_net_admin,cap_net_raw+ep" /packetflix

# -- 2nd stage ---------------------------------------------------------------
#
# Assemble the root filesystem of the final image from Debian stable packages,
# for the target architecture but without running anything under emulation.
# Instead of installing the packages, we only unpack them: this skips their
# maintainer scripts, so in particular dumpcap doesn't get file capabilities;
# packetflix passes on its capabilities as ambient capabilities instead.
#
# We need dumpcap from wireshark-common, but not libwireshark & friends that
# wireshark-common depends on, as dumpcap doesn't link to them. tcpdump would be
# smaller, but it cannot capture from a subset of network interfaces, only from
# "all", which then bangs everything into a single uniform capture mess.
FROM debian AS rootfs

ARG TARGETPLATFORM

RUN dpkg --add-architecture "$(xx-info debian-arch)" && \
    apt-get update && \
    apt-get install -y --no-install-recommends ca-certificates

# dumpcap's shared libraries as listed by ldd, plus libgcc-s1 that glibc dlopens
# for thread cancellation.
ARG DUMPCAP_LIBS="libatomic1 libc6 libcap2 libdbus-1-3 libgcc-s1 libglib2.0-0t64 libnl-3-200 libnl-genl-3-200 libnl-route-3-200 libpcap0.8t64 libpcre2-8-0 libsystemd0 zlib1g"

WORKDIR /debs
RUN arch="$(xx-info debian-arch)" && \
    apt-get download \
        $(for pkg in base-files wireshark-common ${DUMPCAP_LIBS}; do echo "${pkg}:${arch}"; done) \
        ca-certificates tzdata

# Like distroless, add package records for vulnerability scanners, and keep
# only the copyright files from the documentation.
RUN mkdir -p /rootfs && \
    dpkg-deb -x base-files_*.deb /rootfs && \
    mkdir -p /rootfs/var/lib/dpkg/status.d && \
    for deb in *.deb; do \
        pkg="$(dpkg-deb -f "${deb}" Package)"; \
        dpkg-deb -f "${deb}" > "/rootfs/var/lib/dpkg/status.d/${pkg}"; \
        dpkg-deb --ctrl-tarfile "${deb}" | tar -xO ./md5sums > "/rootfs/var/lib/dpkg/status.d/${pkg}.md5sums"; \
        case "${pkg}" in \
        base-files) ;; \
        wireshark-common) \
            dpkg-deb -x "${deb}" /tmp/pkg; \
            cp -a /tmp/pkg/usr/bin/dumpcap /rootfs/usr/bin/; \
            install -D -m 0644 /tmp/pkg/usr/share/doc/${pkg}/copyright /rootfs/usr/share/doc/${pkg}/copyright; \
            rm -rf /tmp/pkg;; \
        ca-certificates) \
            dpkg-deb -x "${deb}" /tmp/pkg; \
            install -D -m 0644 /tmp/pkg/usr/share/doc/${pkg}/copyright /rootfs/usr/share/doc/${pkg}/copyright; \
            install -D -m 0644 /etc/ssl/certs/ca-certificates.crt /rootfs/etc/ssl/certs/ca-certificates.crt; \
            rm -rf /tmp/pkg;; \
        *) dpkg-deb -x "${deb}" /rootfs;; \
        esac; \
    done && \
    find /rootfs/usr/share/doc -type f ! -name copyright -delete && \
    rm -rf /rootfs/usr/share/man && \
    printf 'root:x:0:0:root:/root:/usr/sbin/nologin\nnobody:x:65534:65534:nobody:/nonexistent:/usr/sbin/nologin\n' > /rootfs/etc/passwd && \
    printf 'root:x:0:\nnogroup:x:65534:\n' > /rootfs/etc/group && \
    test -L /rootfs/bin && test -L /rootfs/lib && test -L /rootfs/sbin

# -- final stage -------------------------------------------------------------
#
# Build the final Docker image, containing only our Packetflix application and
# the required additional external tool, namely: dumpcap.
FROM scratch AS final
COPY --from=rootfs /rootfs/ /
COPY --from=builder /packetflix /
# Fails the build if the rootfs lacks any of dumpcap's shared libraries.
RUN ["/usr/bin/dumpcap", "--version"]
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
