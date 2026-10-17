# The Go modules in this repo.
modules := "core vim syntax protocol plugin ."

# List the recipes.
default:
    @just --list

# Run every module's tests with the race detector.
test:
    for m in {{ modules }}; do (cd $m && go test -race ./...) || exit 1; done

# Run go vet on every module, and on the sdk as the wasm it builds into.
vet:
    for m in {{ modules }}; do (cd $m && go vet ./...) || exit 1; done
    cd sdk && GOOS=wasip1 GOARCH=wasm go vet ./...

# Build every module on its own, outside the workspace, as go get and go
# install build them, so a requirement the workspace hides fails here (D37).
standalone:
    for m in {{ modules }}; do (cd $m && GOWORK=off go build ./...) || exit 1; done
    cd sdk && GOWORK=off GOOS=wasip1 GOARCH=wasm go build ./...

# Format every Go file.
fmt:
    gofumpt -w .

# Fail if a Go file is not formatted.
fmt-check:
    test -z "$(gofumpt -l .)"

# Build bin/fude.
build:
    go build -o bin/fude ./cmd/fude

# Write fude's man pages to share/man, for packaging.
man: build
    mkdir -p share/man/man1 share/man/man5 share/man/man7
    bin/fude --man > share/man/man1/fude.1
    bin/fude --man config > share/man/man5/fude-config.5
    bin/fude --man guide > share/man/man7/fude-guide.7

# Build fude and open the files or directories given, or an empty buffer.
run *args: build
    bin/fude {{ args }}

# Build the test plugins, each written with the sdk, with fude plugin build.
wasm:
    for d in plugin/testdata/*/; do go run ./cmd/fude plugin build $d || exit 1; done

# Record the README's demo with VHS (demos/README.md).
demo:
    vhs demos/fude.tape

# Check the speed promises, without the race detector.
perf:
    go test -count=1 -v -run Promise ./ui/term/

# Fail if core/ grows past 10k lines of Go, tests excluded.
kernel-size:
    test $(find core -name '*.go' ! -name '*_test.go' -exec cat {} + | wc -l) -le 10000

# Count lines of Go code by category, without tests, test plugins, blanks
# or comment-only lines.
loc:
    #!/usr/bin/env bash
    set -euo pipefail
    count() { find "$@" -maxdepth 1 -name '*.go' ! -name '*_test.go' -exec cat {} + | grep -cvE '^[[:space:]]*(//|$)'; }
    total=0
    while read -r name dirs; do
        n=$(count $dirs)
        total=$((total + n))
        printf '%-12s %6d  %s\n' "$name" "$n" "$dirs"
    done <<'EOF'
    editor editor
    vim vim
    modules syntax plugin protocol/lsp sdk api
    clients ui/term client
    kernel core/kernel core/rope
    glue cmd/fude pty clipboard
    EOF
    printf '%-12s %6d\n' total "$total"

# Run everything a commit needs.
check: fmt-check vet standalone test kernel-size perf
