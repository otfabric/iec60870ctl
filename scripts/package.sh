#!/bin/sh
# SPDX-License-Identifier: MIT
# package.sh DIR: pack every binary in DIR into DIR/<name>.tar.gz (zip for .exe) and write
# DIR/checksums.txt (SHA-256 of the archives).
set -eu
dir=$1
cd "$dir"
for f in *; do
	case $f in
	*.tar.gz|*.zip|checksums.txt) continue ;;
	*.exe) zip -q "${f%.exe}.zip" "$f" ;;
	*) tar czf "$f.tar.gz" "$f" ;;
	esac
done
if command -v sha256sum >/dev/null 2>&1; then sha256sum ./*.tar.gz ./*.zip | sed 's# \./# #' > checksums.txt
else shasum -a 256 ./*.tar.gz ./*.zip | sed 's# \./# #' > checksums.txt; fi
