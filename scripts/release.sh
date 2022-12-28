#!/bin/bash
set -euo pipefail

trap clean EXIT

function clean {
    test -z ${BUILD_DIR:+x} || rm -rf "$BUILD_DIR"
    rm -f -- *.tar.gz "$SHA256SUMS"
}

if [ $# -ne 1 ]; then
    echo "Usage: $0 RELEASE"
    exit 1
fi

: "${GITHUB_TOKEN:?Need to set environment variable GITHUB_TOKEN}"

ARCHS=("amd64" "arm64")
BINARY=vault-kv-search
BUILD_DIR=$(mktemp -d)
OSES=("linux" "darwin" "windows")
RELEASE=$1
SHA256SUMS=sha256sums.txt

OUTPUT=$(
    curl -s -XPOST \
        -H "Authorization: token $GITHUB_TOKEN" \
        -H "Content-Type: application/json" \
        --data "{\"tag_name\": \"v$RELEASE\"}" \
        https://api.github.com/repos/xbglowx/vault-kv-search/releases
)

RELEASE_ID=$(echo "$OUTPUT" | jq -r '.id')

for os in "${OSES[@]}"; do
    for arch in  "${ARCHS[@]}"; do
        TAR_FILENAME="vault-kv-search-${RELEASE}.${os}-${arch}.tar.gz"
        GOOS=$os GOARCH=$arch go build -o "$BUILD_DIR/$BINARY"
        tar -czvf "$TAR_FILENAME" -C "$BUILD_DIR" "$BINARY"
        curl -XPOST \
            -H "Authorization: token $GITHUB_TOKEN" \
            -H "Content-Type: $(file -b --mime-type "$TAR_FILENAME")" \
            --data-binary @"$TAR_FILENAME" \
            "https://uploads.github.com/repos/xbglowx/vault-kv-search/releases/$RELEASE_ID/assets?name=$TAR_FILENAME"
    done
done

sha256sum -- *.tar.gz > "$SHA256SUMS"
curl -XPOST \
    -H "Authorization: token $GITHUB_TOKEN" \
    -H "Content-Type: $(file -b --mime-type "$TAR_FILENAME")" \
    --data-binary @"$SHA256SUMS" \
    "https://uploads.github.com/repos/xbglowx/vault-kv-search/releases/$RELEASE_ID/assets?name=$SHA256SUMS"
