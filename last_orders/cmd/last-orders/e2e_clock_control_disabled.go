//go:build !e2e

package main

import (
	"fmt"
)

func configureClockControl(lookup func(string) (string, bool), _ clockControlSafety) (clockControlSetup, error) {
	for _, key := range []string{"LAST_ORDERS_E2E_INITIAL_TIME", "LAST_ORDERS_E2E_CLOCK_ADDR"} {
		if _, configured := lookup(key); configured {
			return nil, fmt.Errorf("%s requires a backend built with the e2e Go build tag", key)
		}
	}
	return nil, nil
}
