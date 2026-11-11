#!/bin/sh
# Downloads a portable OpenSSH server (Debian "trixie" packages, unpacked
# without root) into .cache/openssh so the integration tests can run a real
# sshd as a normal user. Linux amd64 only. Skip this when your system already
# has sshd (apt install openssh-server): the tests find /usr/sbin/sshd.
#
# Usage: scripts/fetch-sshd.sh [target-dir]
set -eu

root=$(cd "$(dirname "$0")/.." && pwd)
dest=${1:-$root/.cache/openssh}
mirror=${DEBIAN_MIRROR:-https://deb.debian.org/debian}
suite=trixie

if [ -x "$dest/usr/sbin/sshd" ]; then
	echo "already present: $dest"
	exit 0
fi
command -v curl >/dev/null || { echo "curl is required" >&2; exit 1; }
command -v dpkg >/dev/null || { echo "dpkg is required (only to unpack .deb files)" >&2; exit 1; }
command -v xz >/dev/null || { echo "xz is required" >&2; exit 1; }

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
mkdir -p "$dest"
curl -fsSL "$mirror/dists/$suite/main/binary-amd64/Packages.xz" | xz -d >"$work/Packages"

# openssh-server and the libraries it needs that a minimal host may lack.
packages="openssh-server openssh-client openssh-sftp-server libpam-modules libpam0g
libwrap0 libwtmpdb0 libaudit1 libaudit-common libcap-ng0 libfido2-1 libedit2 libcbor0.10
libgssapi-krb5-2 libkrb5-3 libk5crypto3 libkrb5support0 libkeyutils1 libcom-err2
libselinux1 libpcre2-8-0 libssl3t64 zlib1g libcrypt1 libbsd0 libmd0 libtinfo6"

for p in $packages; do
	file=$(awk -v p="$p" '$0=="Package: "p{f=1} f&&/^Filename:/{print $2; exit}' "$work/Packages")
	[ -n "$file" ] || { echo "package $p not found in $suite" >&2; exit 1; }
	curl -fsSL -o "$work/$p.deb" "$mirror/$file"
	dpkg -x "$work/$p.deb" "$dest"
done
echo "OpenSSH unpacked into $dest"
"$dest/usr/sbin/sshd" -V 2>&1 | head -1 || true
