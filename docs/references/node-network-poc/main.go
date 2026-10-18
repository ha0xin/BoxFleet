// This research program only reads local network state. It does not change
// addresses, routes, interfaces, or network namespaces.
package main

import (
	"fmt"
	"time"

	"github.com/vishvananda/netlink"
)

func main() {
	addresses, err := netlink.AddrList(nil, netlink.FAMILY_ALL)
	if err != nil {
		panic(err)
	}
	fmt.Printf("enumeration_ok address_count=%d\n", len(addresses))

	updates := make(chan netlink.AddrUpdate, 64)
	done := make(chan struct{})
	subscriptionErrors := make(chan error, 4)
	err = netlink.AddrSubscribeWithOptions(updates, done, netlink.AddrSubscribeOptions{
		ListExisting: true,
		ErrorCallback: func(err error) {
			select {
			case subscriptionErrors <- err:
			default:
			}
		},
	})
	if err != nil {
		panic(err)
	}

	timer := time.NewTimer(200 * time.Millisecond)
	defer timer.Stop()
	count := 0
collect:
	for {
		select {
		case _, ok := <-updates:
			if !ok {
				panic("subscription closed before cancellation")
			}
			count++
		case err := <-subscriptionErrors:
			panic(err)
		case <-timer.C:
			break collect
		}
	}

	close(done)
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	// Continue draining until the library closes its output channel. Closing
	// done alone cannot release a send blocked on a full output channel.
	for {
		select {
		case _, ok := <-updates:
			if !ok {
				fmt.Printf("subscription_ok initial_events=%d shutdown_ok=true\n", count)
				return
			}
		case <-deadline.C:
			panic("subscription did not close")
		}
	}
}
