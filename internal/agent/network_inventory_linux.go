//go:build linux

package agent

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"os"
	"strconv"

	"github.com/haoxin/boxfleet/internal/model"
	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

func scanLocalAddresses() ([]model.NodeAddress, error) {
	links, err := netlink.LinkList()
	if err != nil {
		return nil, err
	}
	byIndex := map[int]*netlink.LinkAttrs{}
	for _, l := range links {
		byIndex[l.Attrs().Index] = l.Attrs()
	}
	addresses, err := netlink.AddrList(nil, netlink.FAMILY_ALL)
	if err != nil {
		return nil, err
	}
	if len(addresses) > model.MaxNodeAddresses {
		return nil, fmt.Errorf("more than %d local addresses", model.MaxNodeAddresses)
	}
	out := make([]model.NodeAddress, 0, len(addresses))
	for _, a := range addresses {
		link, ok := byIndex[a.LinkIndex]
		if !ok {
			return nil, fmt.Errorf("interface changed during address scan")
		}
		if a.IPNet == nil {
			return nil, fmt.Errorf("missing address prefix")
		}
		ip, ok := netip.AddrFromSlice(a.IPNet.IP)
		if !ok {
			return nil, fmt.Errorf("invalid interface address")
		}
		ip = ip.Unmap()
		bits, _ := a.IPNet.Mask.Size()
		family := 6
		if ip.Is4() {
			family = 4
		}
		scope := strconv.Itoa(a.Scope)
		switch a.Scope {
		case unix.RT_SCOPE_UNIVERSE:
			scope = "global"
		case unix.RT_SCOPE_LINK:
			scope = "link"
		case unix.RT_SCOPE_HOST:
			scope = "host"
		case unix.RT_SCOPE_SITE:
			scope = "site"
		}
		out = append(out, model.NodeAddress{Address: ip.String(), Family: family, PrefixLength: bits, Interface: link.Name, InterfaceIndex: a.LinkIndex, InterfaceUp: link.Flags&net.FlagUp != 0, Scope: scope, FlagsKnown: true, Temporary: family == 6 && a.Flags&unix.IFA_F_TEMPORARY != 0, Deprecated: a.Flags&unix.IFA_F_DEPRECATED != 0, Tentative: a.Flags&unix.IFA_F_TENTATIVE != 0, DADFailed: a.Flags&unix.IFA_F_DADFAILED != 0})
	}
	return out, nil
}

// Changes are hints. One consumer always drains library channels, including
// after cancellation, so a full library send cannot prevent shutdown.
func watchLocalAddresses(ctx context.Context) <-chan struct{} {
	notifications := make(chan struct{}, 1)
	go func() {
		defer close(notifications)
		done := make(chan struct{})
		stopping := false
		stop := func() {
			if !stopping {
				close(done)
				stopping = true
			}
		}
		defer stop()
		errorsCh := make(chan error, 1)
		onError := func(err error) {
			select {
			case errorsCh <- err:
			default:
			}
		}
		addresses := make(chan netlink.AddrUpdate, 64)
		links := make(chan netlink.LinkUpdate, 16)
		if err := netlink.AddrSubscribeWithOptions(addresses, done, netlink.AddrSubscribeOptions{ErrorCallback: onError}); err != nil {
			fmt.Fprintf(os.Stderr, "boxfleet-agent address subscription unavailable; polling: %v\n", err)
			return
		}
		if err := netlink.LinkSubscribeWithOptions(links, done, netlink.LinkSubscribeOptions{ErrorCallback: onError}); err != nil {
			fmt.Fprintf(os.Stderr, "boxfleet-agent link subscription unavailable; polling: %v\n", err)
			stop()
			links = nil
		}
		cancelled := ctx.Done()
		for addresses != nil || links != nil {
			select {
			case _, ok := <-addresses:
				if !ok {
					addresses = nil
					stop()
				} else if !stopping {
					select {
					case notifications <- struct{}{}:
					default:
					}
				}
			case _, ok := <-links:
				if !ok {
					links = nil
					stop()
				} else if !stopping {
					select {
					case notifications <- struct{}{}:
					default:
					}
				}
			case err := <-errorsCh:
				if !stopping && ctx.Err() == nil {
					fmt.Fprintf(os.Stderr, "boxfleet-agent network subscription failed; polling: %v\n", err)
				}
				stop()
			case <-cancelled:
				stop()
				cancelled = nil
			}
		}
	}()
	return notifications
}
