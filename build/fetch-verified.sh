#!/bin/sh
# 下载到临时文件，对照 dependency-checksums.txt 校验 SHA256，通过后才解压。
# 用法: fetch-verified.sh URL FILENAME DEST SUMS tar.gz|tar.xz
set -eu
if [ "$#" -ne 5 ]; then
	echo "usage: fetch-verified.sh URL FILENAME DEST SUMS tar.gz|tar.xz" >&2
	exit 2
fi
url=$1
filename=$2
dest=$3
sums=$4
format=$5
mkdir -p "$dest"
workdir=$(mktemp -d)
trap 'rm -rf "$workdir"' EXIT
curl -fsSL "$url" -o "$workdir/$filename"
sumline=$(awk -v name="$filename" '$2 == name { print; found = 1; exit } END { if (!found) exit 1 }' "$sums") || {
	echo "没有已核实的 SHA256，拒绝解压: $filename" >&2
	exit 1
}
printf '%s\n' "$sumline" > "$workdir/SHA256SUMS"
if command -v sha256sum >/dev/null 2>&1; then
	(cd "$workdir" && sha256sum -c SHA256SUMS)
else
	(cd "$workdir" && shasum -a 256 -c SHA256SUMS)
fi
case "$format" in
tar.gz) tar -xzf "$workdir/$filename" -C "$dest" ;;
tar.xz) tar -xJf "$workdir/$filename" -C "$dest" ;;
*)
	echo "不支持的压缩格式: $format" >&2
	exit 2
	;;
esac
