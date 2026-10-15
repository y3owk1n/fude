# AGENTS.md

Shared guide for every AI agent in this repo. `CLAUDE.md` is a symlink to
this file, so edit here.

fude is a modal text editor in Go that works the way Neovim does, without
its weight. The spec is `prd/`: start with `prd/product.md`, then
`prd/architecture.md`. `prd/behaviours.md` is what fude promises, and
`prd/decisions.md` is why it is shaped this way.

## Toolchain

`oku sync && oku allow` installs Go and the tools in `oku.toml`. Add a tool
there, never with a system package manager.

## How to write code here

This repo is production grade, and the way to get there is less code.

- Solve the problem in front of you at its simplest. Write the least code
  that does it correctly, then stop.
- No abstraction without a second use. An interface exists only at a
  boundary the spec names (`vim.Text`, `vim.Host`, the kernel's `Store` and
  `Listener`, `api/`) or once two implementations exist.
- No speculative options, flags, hooks or generality. Build for the
  behaviour being implemented, not the ones after it.
- Prefer the standard library. A new dependency is a decision: add it to
  `prd/decisions.md` with why the standard library is not enough.
- Delete before adding. A change that removes code is better than one that
  works around it.
- Errors carry context (`fmt.Errorf("open %s: %w", path, err)`) and are
  handled once. Never swallow one.

## Boundaries that CI enforces

- `core/` imports only the standard library, starts no goroutines and does
  no I/O.
- `vim/` never imports `core/`, and `core/` never imports `vim/`.
- Modules import `api/`, never `core/` or `vim/`.
- cgo lives only in `syntax/`.

## Tests

Test only what `prd/behaviours.md` promises, through a public boundary:
keys into a headless buffer, the CLI, a file on disk, the exported API of
`core/` and `vim/`, the plugin ABI or the RPC protocol. Name the B-number
the test covers. Mock only at system boundaries.

Keep this file short. It loads into every session. Add only rules an agent
cannot infer from the code or the spec.
