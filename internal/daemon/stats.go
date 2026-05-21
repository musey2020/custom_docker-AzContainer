// Package daemon — stats RPC və panic recovery.
package daemon

import (
	"azcontainer/internal/log"
	"azcontainer/internal/metrics"
	"fmt"
	"runtime/debug"
)

// StatsReply — RPC stats response.
type StatsReply struct {
	Snapshot *metrics.Snapshot
}

// Stats — verilmiş container üçün resurs snapshot-u qaytarır.
func (a *API) Stats(args *IDArgs, reply *StatsReply) error {
	c, err := a.store.FindByPrefix(args.ID)
	if err != nil {
		return err
	}

	snap, err := metrics.Collect(c.ID)
	if err != nil {
		return fmt.Errorf("stats: %w", err)
	}
	reply.Snapshot = snap
	return nil
}

// recoverPanic — RPC method-larında panic-i tutmaq üçün defer-də işlədilir.
//
// Daemon-un bütün proses ölməsinin qarşısını alır.
func recoverPanic(method string) {
	if r := recover(); r != nil {
		log.Error("RPC panic",
			"method", method,
			"panic", fmt.Sprintf("%v", r),
			"stack", string(debug.Stack()),
		)
	}
}
