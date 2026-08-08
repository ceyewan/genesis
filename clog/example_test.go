package clog_test

import "github.com/ceyewan/genesis/clog"

func Example() {
	logger := clog.Discard()
	logger.Info("application started")
}
