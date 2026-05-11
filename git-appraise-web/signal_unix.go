//go:build !windows

package main

import (
	"os"
	"os/signal"
	"syscall"
)

func setupReloadOnSignal(repos *Repos) {
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGUSR1)
	go func() {
		for range sigs {
			repos.Discover()
		}
	}()
}
