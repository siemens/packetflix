#!/usr/bin/env bash
# Makes the CA certificates that VS Code exports from the host's certificate
# store available to all programs in this dev container, not only to Node.js
# (which picks them up via NODE_EXTRA_CA_CERTS). Without such an export, such as
# on hosts without VS Code or when using the devcontainer CLI, this is a no-op.
set -euo pipefail

DEST=/usr/local/share/ca-certificates/vscode-host

bundle="${NODE_EXTRA_CA_CERTS:-}"
if [[ -z "${bundle}" || ! -r "${bundle}" ]]; then
    # Lifecycle commands don't get NODE_EXTRA_CA_CERTS, so look for the most
    # recent export of a VS Code (Insiders) server instead.
    bundle=$(ls -t "${HOME}"/.vscode-server*/data/certificates/certificates-*.pem 2>/dev/null | head -n 1 || true)
fi

if [[ -z "${bundle}" ]]; then
    if [[ -d "${DEST}" ]]; then
        sudo rm -rf "${DEST}"
        sudo update-ca-certificates >/dev/null
    fi
    echo "host CA certificates: none exported by VS Code, nothing to do"
    exit 0
fi

tmp=$(mktemp -d)
trap 'rm -rf "${tmp}"' EXIT
awk -v dir="${tmp}" '
    /-----BEGIN CERTIFICATE-----/ { n++; f = sprintf("%s/host-%04d.crt", dir, n) }
    f { print > f }
    /-----END CERTIFICATE-----/ { close(f); f = "" }
' "${bundle}"

count=$(find "${tmp}" -name '*.crt' | wc -l)
sudo rm -rf "${DEST}"
sudo install -d -m 0755 "${DEST}"
if (( count > 0 )); then
    sudo install -m 0644 "${tmp}"/*.crt "${DEST}/"
fi
sudo update-ca-certificates >/dev/null 2>&1
echo "host CA certificates: ${count} installed from ${bundle}"
