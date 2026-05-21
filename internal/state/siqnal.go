// Package state — signal helpers (Linux-spesifik).
package state

import "syscall"

// syscallSignal0 — proses mövcudluğunu yoxlamaq üçün.
var syscallSignal0 = syscall.Signal(0)
