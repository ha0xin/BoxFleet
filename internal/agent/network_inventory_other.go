//go:build !linux

package agent

import (
	"context"
	"fmt"
	"net"
	"net/netip"

	"github.com/haoxin/boxfleet/internal/model"
)

func scanLocalAddresses() ([]model.NodeAddress, error) {
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	out := []model.NodeAddress{}
	for _, iface := range interfaces {
		addresses, err := iface.Addrs()
		if err != nil {
			return nil, err
		}
		for _, address := range addresses {
			p, err := netip.ParsePrefix(address.String())
			if err != nil {
				return nil, err
			}
			ip := p.Addr()
			family := 6
			if ip.Is4() {
				family = 4
			}
			out = append(out, model.NodeAddress{Address: ip.String(), Family: family, PrefixLength: p.Bits(), Interface: iface.Name, InterfaceIndex: iface.Index, InterfaceUp: iface.Flags&net.FlagUp != 0, Scope: "unknown"})
			if len(out) > model.MaxNodeAddresses {
				return nil, fmt.Errorf("too many local addresses")
			}
		}
	}
	return out, nil
}

func watchLocalAddresses(context.Context) <-chan struct{} { return nil }
