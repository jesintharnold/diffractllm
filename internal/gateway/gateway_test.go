package gateway

import "testing"

// server.Shutdown races httpServer.Shutdown(ctx) against StreamGrace. If the
// ctx deadline lands first, cancelBase never runs: open streams are severed at
// process exit without reaching the settle path, so their usage is never billed.
func TestStreamGraceFitsInsideShutdownBudget(t *testing.T) {
	if StreamGrace >= ShutdownTimeout {
		t.Fatalf("StreamGrace (%s) must be shorter than ShutdownTimeout (%s): "+
			"otherwise the http deadline fires first, cancelBase never runs, "+
			"and open streams are cut without settling",
			StreamGrace, ShutdownTimeout)
	}
}
