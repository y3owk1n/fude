//go:build !cgo

package syntax

// tree-sitter's binding is C, so fude builds only with cgo and a C
// compiler: Xcode's on macOS, gcc or clang on Linux, and on Windows the
// llvm-mingw oku sync installs. Without cgo this name says so.
type _ fude_needs_cgo_and_a_C_compiler_on_PATH
