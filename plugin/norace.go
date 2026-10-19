//go:build !race

package plugin

// slowdown widens the budgets under the race detector, and is 1 without it.
const slowdown = 1
