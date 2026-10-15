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

# Build and open a file in fude.
run path: build
    bin/fude {{ path }}

# Run everything a commit needs.
check: fmt-check vet test
