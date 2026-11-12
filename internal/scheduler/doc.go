//go:build (schedulerfixture || privateworker) && linux

// Package scheduler contains the shared bounded Linux scheduling protocol.
// Only explicit schedulerfixture/privateworker builds include it. It must never be imported by the HTTP API.
package scheduler
