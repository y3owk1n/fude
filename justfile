# The Go modules in this repo.
modules := "api core vim frontend syntax protocol plugin . cmd/fude"

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
    # cmd/fude would take the others at their last release, so the release
    # workflow's go install checks it instead.
    for m in {{ modules }}; do [ $m = cmd/fude ] || (cd $m && GOWORK=off go build ./...) || exit 1; done
    cd sdk && GOWORK=off GOOS=wasip1 GOARCH=wasm go build ./...
    # A frontend builds with no C compiler (D64).
    cd frontend && GOWORK=off CGO_ENABLED=0 go build ./...

# Format every Go file.
fmt:
    gofumpt -w .

# Fail if a Go file is not formatted.
fmt-check:
    test -z "$(gofumpt -l .)"

# Build bin/fude.
build:
    go build -o bin/fude ./cmd/fude

# Build bin/Fude.app, fude in a macOS app that opens in its window,
# unsigned and with no icon (B565).
app: build
    sh gui/app/bundle.sh bin/fude bin/Fude.app

# Write fude's man pages to share/man, for packaging.
man: build
    mkdir -p share/man/man1 share/man/man5 share/man/man7
    bin/fude man > share/man/man1/fude.1
    bin/fude man config > share/man/man5/fude-config.5
    bin/fude man guide > share/man/man7/fude-guide.7

# Build fude and open the files or directories given, or an empty buffer.
[positional-arguments]
run *args: build
    bin/fude "$@"

# Build the test plugins, each written with the sdk, with fude plugin build.
wasm:
    for d in plugin/testdata/*/; do go run ./cmd/fude plugin build $d || exit 1; done

# Write the resource that gives fude's Windows window the system's current
# common controls (D69), linked into every Windows binary that imports gui.
winres:
    go-winres simply --arch amd64,arm64 --manifest gui --out gui/rsrc

# Record a README demo with VHS, fude.tape unless tape names another (demos/README.md).
demo tape="fude":
    vhs demos/{{tape}}.tape

# Check the speed and memory promises, without the race detector, one
# package at a time so none slows another's timings.
perf: quiet
    go test -count=1 -p 1 -v -run Promise ./tui/ ./editor/
    cd cmd/fude && go test -count=1 -v -run Promise .

# Fail when the Mac is too busy to time fude. A locked screen slows every
# process. Over a fifth of the CPU in use moves fude onto slower cores and
# takes the CPU from it mid-key. It samples one second before the run, so
# load that starts during the run still shows in the timings.
quiet:
    #!/usr/bin/env bash
    set -euo pipefail
    [ "$(uname)" = Darwin ] || exit 0
    locked=$(ioreg -n Root -d1 -a | grep -A1 CGSSessionScreenIsLocked || true)
    if [[ $locked == *true* ]]; then
        echo "the screen is locked, which slows the timings. Rerun just perf with it unlocked." >&2
        exit 1
    fi
    # top's first sample averages since boot, so this reads the second.
    sample=$(top -l 2 -n 3 -o cpu -s 1 -stats cpu,command | awk '/CPU usage/ { n++ } n == 2')
    idle=$(awk '/CPU usage/ { print int($7) }' <<< "$sample")
    if [ "$idle" -lt 80 ]; then
        echo "the CPU is $((100 - idle))% busy, and the timings need it under 20%. Rerun just perf when these are done:" >&2
        awk '/%CPU/ { on = 1; next } on' <<< "$sample" >&2
        exit 1
    fi

# Tidy cmd/fude's go.mod against the checkout, and fail if that changed it.
# go install of a release takes the versions go.mod lists, and the newest
# version of any module it lacks (D37).
cmd-tidy:
    #!/usr/bin/env bash
    set -euo pipefail
    cd cmd/fude
    replace=(); drop=()
    for m in "" /api /core /vim /frontend /syntax /protocol /plugin; do
        replace+=("-replace=github.com/y3owk1n/fude$m=../..$m")
        drop+=("-dropreplace=github.com/y3owk1n/fude$m")
    done
    go mod edit "${replace[@]}"
    GOWORK=off go mod tidy
    go mod edit "${drop[@]}"
    git diff --exit-code -- go.mod go.sum

# Fail if a package outside syntax/ uses cgo.
cgo:
    test -z "$(for m in {{ modules }}; do (cd $m && go list -f '{{{{if .CgoFiles}}{{{{.ImportPath}}{{{{end}}' ./...); done | grep -vx github.com/y3owk1n/fude/syntax)"

# Fail if a package in modules/ imports a fude package other than api/.
module-imports:
    test -z "$(go list -f '{{{{join .Imports "\n"}}' ./modules/... | grep '^github.com/y3owk1n/fude/' | grep -vE '^github.com/y3owk1n/fude/(api|protocol|internal)')"

# Fail if core/ grows past 10k lines of Go, tests excluded.
kernel-size:
    test $(find core -name '*.go' ! -name '*_test.go' -exec cat {} + | wc -l) -le 10000

# Count the lines of the Go files git tracks, by the layers of
# prd/architecture.md, without blank lines. A comment line is one that
# starts with //. Tests are _test.go files, the test helpers and the test
# plugins. The native window splits into what every OS shares and each
# OS's own files. It also counts the lines of the help pages, the
# tree-sitter queries and the spec.
loc:
    #!/usr/bin/env bash
    set -euo pipefail
    git ls-files '*.go' | awk '
    BEGIN {
        n = split("kernel vim editor api plugins modules syntax protocols frontend tui gui gui_darwin gui_linux gui_windows support", order, " ")
        name["kernel"] = "kernel (core)"; name["vim"] = "vim machine"; name["editor"] = "editor"
        name["api"] = "module API"; name["plugins"] = "plugin host and SDK"; name["modules"] = "modules"
        name["syntax"] = "syntax (cgo)"; name["protocols"] = "LSP and DAP"; name["frontend"] = "frontend"
        name["tui"] = "terminal UI"; name["gui"] = "window, shared"; name["gui_darwin"] = "window, macOS"
        name["gui_linux"] = "window, Linux"; name["gui_windows"] = "window, Windows"; name["support"] = "internal and main"
    }
    {
        file = $0
        split(file, part, "/")
        g = part[1]
        if (g == "core") g = "kernel"
        else if (g == "plugin" || g == "sdk") g = "plugins"
        else if (g == "distribution") g = "modules"
        else if (g == "protocol") g = "protocols"
        else if (g == "internal" || g == "cmd") g = "support"
        else if (g == "gui" && match(file, /_(darwin|linux|windows)[_.]/)) g = "gui_" substr(file, RSTART + 1, RLENGTH - 2)
        # A folder no layer names yet still counts, under its own name.
        if (!(g in name)) { name[g] = g; order[++n] = g }
        test = file ~ /_test\.go$/ || file ~ /\/(editortest|plugintest|syntaxtest|testdata)\//
        while ((getline line < file) > 0) {
            if (line ~ /^[[:space:]]*$/) continue
            if (test) tests[g]++
            else if (line ~ /^[[:space:]]*\/\//) comments[g]++
            else code[g]++
        }
        close(file)
    }
    END {
        printf "%-22s %7s %9s %7s\n", "", "code", "comments", "tests"
        for (i = 1; i <= n; i++) {
            g = order[i]
            printf "%-22s %7d %9d %7d\n", name[g], code[g], comments[g], tests[g]
            c += code[g]; m += comments[g]; t += tests[g]
        }
        printf "%-22s %7d %9d %7d\n", "total", c, m, t
    }'
    lines() { git ls-files "$@" | xargs cat | grep -cv '^[[:space:]]*$'; }
    echo
    printf '%-22s %7d\n' "help pages" "$(lines 'editor/reference/*.md' 'editor/guide/*.md')"
    printf '%-22s %7d\n' "tree-sitter queries" "$(lines '*.scm')"
    printf '%-22s %7d\n' "spec (prd)" "$(lines 'prd/*.md')"

# Run everything a commit needs.
check: ci perf

# Run every check but the speed promises, whose timings vary too much on shared CI runners.
ci: fmt-check vet standalone cmd-tidy cgo module-imports test kernel-size
