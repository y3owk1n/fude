# The Go modules in this repo.
modules := "core vim ."

# List the recipes.
default:
    @just --list

# Run every module's tests with the race detector.
test:
    for m in {{ modules }}; do (cd $m && go test -race ./...) || exit 1; done

# Run go vet on every module.
vet:
    for m in {{ modules }}; do (cd $m && go vet ./...) || exit 1; done

# Format every Go file.
fmt:
    gofumpt -w .

# Fail if a Go file is not formatted.
fmt-check:
    test -z "$(gofumpt -l .)"

# Build bin/fude.
build:
    go build -o bin/fude ./cmd/fude

# Write fude's man page to share/man/man1/fude.1, for packaging.
man: build
    mkdir -p share/man/man1 && bin/fude --man > share/man/man1/fude.1

# Build fude and open the files or directories given, or an empty buffer.
run *args: build
    bin/fude {{ args }}

# Build the test plugins from their .wat files.
wasm:
    for f in plugin/testdata/*.wat; do wasm-tools parse $f -o ${f%.wat}.wasm; done

# Check the speed promises, without the race detector.
perf:
    go test -count=1 -v -run Promise ./ui/term/ | grep -v '^=== RUN'

# Fail if core/ grows past 10k lines of Go, tests excluded.
kernel-size:
    test $(find core -name '*.go' ! -name '*_test.go' -exec cat {} + | wc -l) -le 10000

# Run everything a commit needs.
check: fmt-check vet test kernel-size perf
