// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of Cilium

package lbmap

import (
	"fmt"
	"net"

	"golang.org/x/sys/unix"

	"github.com/cilium/cilium/pkg/bpf"
	"github.com/cilium/cilium/pkg/byteorder"
	"github.com/cilium/cilium/pkg/cidr"
	"github.com/cilium/cilium/pkg/ebpf"
	"github.com/cilium/cilium/pkg/metrics"
	"github.com/cilium/cilium/pkg/option"
	"github.com/cilium/cilium/pkg/types"
)

const (
	SourceRange4MapName = "cilium_lb4_source_range"
	SourceRange6MapName = "cilium_lb6_source_range"
	lpmPrefixLen4       = 16 + 16 // sizeof(SourceRangeKey4.RevNATID)+sizeof(SourceRangeKey4.Pad)
	lpmPrefixLen6       = 16 + 16 // sizeof(SourceRangeKey6.RevNATID)+sizeof(SourceRangeKey6.Pad)
)

type SourceRangeKey interface {
	bpf.MapKey

	GetCIDR() *cidr.CIDR
	GetRevNATID() uint16

	// Convert fields to network byte order.
	ToNetwork() SourceRangeKey

	// ToHost converts fields to host byte order.
	ToHost() SourceRangeKey
}

// The compile-time check for whether the structs implement the interface
var _ SourceRangeKey = (*SourceRangeKey4)(nil)
var _ SourceRangeKey = (*SourceRangeKey6)(nil)

type SourceRangeKey4 struct {
	PrefixLen uint32     `align:"lpm_key"`
	RevNATID  uint16     `align:"rev_nat_id"`
	Pad       uint16     `align:"pad"`
	Address   types.IPv4 `align:"addr"`
}

func (k *SourceRangeKey4) String() string {
	kHost := k.ToHost().(*SourceRangeKey4)
	return fmt.Sprintf("%s (%d)", kHost.GetCIDR().String(), kHost.GetRevNATID())
}

func (k *SourceRangeKey4) New() bpf.MapKey { return &SourceRangeKey4{} }

func (k *SourceRangeKey4) ToNetwork() SourceRangeKey {
	n := *k
	// For some reasons rev_nat_index is stored in network byte order in
	// the SVC BPF maps
	n.RevNATID = byteorder.HostToNetwork16(n.RevNATID)
	return &n
}

// ToHost returns the key in the host byte order
func (k *SourceRangeKey4) ToHost() SourceRangeKey {
	h := *k
	h.RevNATID = byteorder.NetworkToHost16(h.RevNATID)
	return &h
}

func (k *SourceRangeKey4) GetCIDR() *cidr.CIDR {
	var (
		c  net.IPNet
		ip types.IPv4
	)
	c.Mask = net.CIDRMask(int(k.PrefixLen)-lpmPrefixLen4, 32)
	k.Address.DeepCopyInto(&ip)
	c.IP = ip.IP()
	return cidr.NewCIDR(&c)
}
func (k *SourceRangeKey4) GetRevNATID() uint16 {
	return k.RevNATID
}

type SourceRangeKey6 struct {
	PrefixLen uint32     `align:"lpm_key"`
	RevNATID  uint16     `align:"rev_nat_id"`
	Pad       uint16     `align:"pad"`
	Address   types.IPv6 `align:"addr"`
}

func (k *SourceRangeKey6) String() string {
	kHost := k.ToHost().(*SourceRangeKey6)
	return fmt.Sprintf("%s (%d)", kHost.GetCIDR().String(), kHost.GetRevNATID())
}

func (k *SourceRangeKey6) New() bpf.MapKey { return &SourceRangeKey6{} }

func (k *SourceRangeKey6) ToNetwork() SourceRangeKey {
	n := *k
	// For some reasons rev_nat_index is stored in network byte order in
	// the SVC BPF maps
	n.RevNATID = byteorder.HostToNetwork16(n.RevNATID)
	return &n
}

// ToHost returns the key in the host byte order
func (k *SourceRangeKey6) ToHost() SourceRangeKey {
	h := *k
	h.RevNATID = byteorder.NetworkToHost16(h.RevNATID)
	return &h
}

func (k *SourceRangeKey6) GetCIDR() *cidr.CIDR {
	var (
		c  net.IPNet
		ip types.IPv6
	)
	c.Mask = net.CIDRMask(int(k.PrefixLen)-lpmPrefixLen6, 128)
	k.Address.DeepCopyInto(&ip)
	c.IP = ip.IP()
	return cidr.NewCIDR(&c)
}
func (k *SourceRangeKey6) GetRevNATID() uint16 {
	return k.RevNATID
}

type SourceRangeValue struct {
	Pad uint8 // not used
}

func (v *SourceRangeValue) String() string    { return "" }
func (v *SourceRangeValue) New() bpf.MapValue { return &SourceRangeValue{} }

var (
	// SourceRange4Map is the BPF map for storing IPv4 service source ranges to
	// check if option.Config.EnableSVCSourceRangeCheck is enabled.
	SourceRange4Map *bpf.Map
	// SourceRange6Map is the BPF map for storing IPv6 service source ranges to
	// check if option.Config.EnableSVCSourceRangeCheck is enabled.
	SourceRange6Map *bpf.Map
)

// initSourceRange creates the BPF maps for storing both IPv4 and IPv6
// service source ranges.
func initSourceRange(registry *metrics.Registry, params InitParams) {
	SourceRangeMapMaxEntries = params.SourceRangeMapMaxEntries

	if params.IPv4 {
		SourceRange4Map = bpf.NewMap(
			SourceRange4MapName,
			ebpf.LPMTrie,
			&SourceRangeKey4{},
			&SourceRangeValue{},
			SourceRangeMapMaxEntries,
			unix.BPF_F_NO_PREALLOC,
		).WithCache().WithPressureMetric(registry).
			WithEvents(option.Config.GetEventBufferConfig(SourceRange4MapName))
	}

	if params.IPv6 {
		SourceRange6Map = bpf.NewMap(
			SourceRange6MapName,
			ebpf.LPMTrie,
			&SourceRangeKey6{},
			&SourceRangeValue{},
			SourceRangeMapMaxEntries,
			unix.BPF_F_NO_PREALLOC,
		).WithCache().WithPressureMetric(registry).
			WithEvents(option.Config.GetEventBufferConfig(SourceRange6MapName))
	}
}

func srcRangeKey(cidr *cidr.CIDR, revNATID uint16, ipv6 bool) bpf.MapKey {
	ones, _ := cidr.Mask.Size()
	id := byteorder.HostToNetwork16(revNATID)
	if ipv6 {
		key := &SourceRangeKey6{PrefixLen: uint32(ones) + lpmPrefixLen6, RevNATID: id}
		copy(key.Address[:], cidr.IP.To16())
		return key
	} else {
		key := &SourceRangeKey4{PrefixLen: uint32(ones) + lpmPrefixLen4, RevNATID: id}
		copy(key.Address[:], cidr.IP.To4())
		return key
	}
}
