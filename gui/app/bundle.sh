#!/bin/sh
# bundle.sh puts the fude at $1 in Fude.app at $2, for just app, a release's
# zip and oku's builds (B565).
set -eu
here=$(dirname "$0")
rm -rf "$2"
mkdir -p "$2/Contents/MacOS"
cp "$here/Info.plist" "$2/Contents/"
cp "$here/fude-app" "$2/Contents/MacOS/"
cp "$1" "$2/Contents/MacOS/fude"
