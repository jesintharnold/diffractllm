package main

import (
	"fmt"
	"os"

	"diffractllm/internal/gateway"
	logger "diffractllm/internal/observability/logging"
)

func main() {
	gw := gateway.NewGateway()

	if err := gw.Initialize(); err != nil {
		fmt.Fprintln(os.Stderr, "Gateway initialization failed:")
		logger.PrintAggregatedErrors(err)
		if err := gw.Stop(); err != nil {
			fmt.Fprintf(os.Stderr, "Error while stopping gateway after failure: %v\n", err)
		}
		os.Exit(1)
	}

	if err := gw.Start(); err != nil {
		fmt.Fprintln(os.Stderr, "Gateway start failed:")
		logger.PrintAggregatedErrors(err)
		if err := gw.Stop(); err != nil {
			fmt.Fprintf(os.Stderr, "Error while stopping gateway after failure: %v\n", err)
		}
		os.Exit(1)
	}

	fmt.Println("Gateway running. Waiting for interrupt (SIGINT/SIGTERM) to shutdown...")
	gw.WaitForSignal()
}
