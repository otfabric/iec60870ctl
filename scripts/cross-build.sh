#!/bin/sh
# SPDX-License-Identifier: MIT
# cross-build.sh TOOL VERSION LDFLAGS "ARCHS" OUTDIR: build cmd/TOOL for every os/arch of
# ARCHS (os/arch or os/arm/v7) into OUTDIR/TOOL-VERSION-os-arch[.exe]; arm/v7 is "armv7".
set -eu
tool=$1 version=$2 ldflags=$3 archs=$4 out=$5
mkdir -p "$out"
for a in $archs; do
	os=${a%%/*}; rest=${a#*/}; cpu=${rest%%/*}; variant=${rest#*/}
	name="$tool-$version-$os-$cpu"; goarm=""
	if [ "$cpu" = arm ] && [ "$variant" = v7 ]; then name="$tool-$version-$os-armv7"; goarm=7; fi
	[ "$os" = windows ] && name="$name.exe"
	echo "Building $name"
	# shellcheck disable=SC2086 # ldflags holds -ldflags "..." as make quotes it
	eval "CGO_ENABLED=0 GOOS=$os GOARCH=$cpu GOARM=$goarm go build -trimpath $ldflags -o \"$out/$name\" ./cmd/$tool"
done
