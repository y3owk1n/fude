//go:build race

package plugin

// slowdown widens the budgets under the race detector, which slows code
// several times over. The speed promises skip it for the same reason.
const slowdown = 5
