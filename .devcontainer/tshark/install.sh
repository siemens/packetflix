#!/usr/bin/env bash
set -euo pipefail

export DEBIAN_FRONTEND=noninteractive

# Answer the "allow non-superusers to capture" prompt upfront; tests only read pipes and files.
echo "wireshark-common wireshark-common/install-setuid boolean false" | debconf-set-selections

apt-get update
apt-get install -y --no-install-recommends tshark libcap2-bin
rm -rf /var/lib/apt/lists/*

# Like in the image, dumpcap must get its capabilities from packetflix via ambient caps.
if [[ -n "$(getcap /usr/bin/dumpcap)" ]]; then
    setcap -r /usr/bin/dumpcap
fi

tshark --version
