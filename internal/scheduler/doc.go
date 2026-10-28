//go:build schedulerfixture && linux

// Package scheduler implements the isolated Linux fixture controller. It is
// excluded from ordinary builds. It must never be imported by the HTTP API.
package scheduler
