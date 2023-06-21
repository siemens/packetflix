#!/bin/sh
set -e

# Note: requires qemu to be installed. On Debian-based distros, install using
# "sudo apt-get install -y qemu qemu-user-static". Ubuntu starting with 23.04,
# use "sudo apt-get install -y qemu-system qemu-user-static" instead.

PLATFORMS="linux/amd64,linux/arm64"

TAG="packetflix"
DEPLOYMENT="./deployments/${TAG}"

BOBTHEBUILDER="builderbob"
LOCALREGISTRYPORT=5999

# Had our builder been created in the past and still exists?
echo "🔎  checking for builder..."
if ! docker buildx inspect "${BOBTHEBUILDER}" 2>/dev/null 1>&2; then
    echo "👨‍🏭  creating builder..."
    # https://github.com/docker/buildx/issues/835
    docker buildx create --name "${BOBTHEBUILDER}" \
        --bootstrap \
        --platform "${PLATFORMS}" \
        --driver-opt network=host --buildkitd-flags "--allow-insecure-entitlement network.host"
fi

echo "🔎  ensuring local registry is up..."
docker start registry || docker run -d -p ${LOCALREGISTRYPORT}:5000 --restart=always --name registry registry:2

echo "🏗  building..."
rm -rf dist/
mkdir -p dist/
./scripts/docker-build.sh \
    "${DEPLOYMENT}/Dockerfile" \
    -t localhost:${LOCALREGISTRYPORT}/${TAG} \
    --builder "${BOBTHEBUILDER}" --platform "${PLATFORMS}" \
    #--push \
    --output ype=oci,output=dist \
    --network host \
    "$@"
