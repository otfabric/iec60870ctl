// SPDX-License-Identifier: MIT

package iec

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"net"
	"net/netip"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/otfabric/go-iec104/apci"
	"github.com/otfabric/iec104ctl/internal/config"
	"github.com/otfabric/iec104ctl/internal/types"
)

// MaxDiscoverHosts is the number of hosts `discover` probes without --force-large-scan.
const MaxDiscoverHosts = 4096

// defaultProbeTimeout bounds one probe when --timeout is 0.
const defaultProbeTimeout = time.Second

// ExpandHosts turns the --subnets values into the list of addresses to probe: a CIDR prefix
// (network and broadcast address left out for IPv4 prefixes shorter than /31), a range on
// the last octet ("192.168.1.10-20"), a single address or a host name.
func ExpandHosts(specs []string, limit int) ([]string, error) {
	var out []string
	seen := map[string]bool{}
	add := func(h string) error {
		if seen[h] {
			return nil
		}
		if limit > 0 && len(out) >= limit {
			return fmt.Errorf("more than %d hosts to probe: narrow --subnets, or pass --force-large-scan", limit)
		}
		seen[h] = true
		out = append(out, h)
		return nil
	}
	for _, spec := range specs {
		spec = strings.TrimSpace(spec)
		if spec == "" {
			continue
		}
		switch {
		case strings.Contains(spec, "/"):
			prefix, err := netip.ParsePrefix(spec)
			if err != nil {
				return nil, fmt.Errorf("invalid subnet %q: %w", spec, err)
			}
			if !prefix.Addr().Is4() {
				return nil, fmt.Errorf("invalid subnet %q: only IPv4 prefixes can be swept; give IPv6 hosts one by one", spec)
			}
			prefix = prefix.Masked()
			first := binary.BigEndian.Uint32(prefix.Addr().AsSlice())
			size := uint64(1) << (32 - prefix.Bits())
			lo, hi := uint64(0), size-1
			if prefix.Bits() < 31 {
				lo, hi = 1, size-2
			}
			if limit > 0 && hi-lo+1 > uint64(limit) {
				return nil, fmt.Errorf("subnet %s has %d hosts, more than %d: narrow --subnets, or pass --force-large-scan", spec, hi-lo+1, limit)
			}
			for i := lo; i <= hi; i++ {
				var b [4]byte
				binary.BigEndian.PutUint32(b[:], first+uint32(i))
				if err := add(netip.AddrFrom4(b).String()); err != nil {
					return nil, err
				}
			}
		case strings.Contains(spec, "-") && strings.Count(spec, ".") == 3:
			base, last, _ := strings.Cut(spec, "-")
			addr, err := netip.ParseAddr(base)
			end, perr := strconv.ParseUint(last, 10, 8)
			if err != nil || perr != nil || !addr.Is4() {
				return nil, fmt.Errorf("invalid range %q: use the form 192.168.1.10-20", spec)
			}
			b := addr.As4()
			if end > math.MaxUint8 || byte(end) < b[3] {
				return nil, fmt.Errorf("invalid range %q: the end is below the start", spec)
			}
			for i := int(b[3]); i <= int(end); i++ {
				b[3] = byte(i & 0xFF)
				if err := add(netip.AddrFrom4(b).String()); err != nil {
					return nil, err
				}
			}
		default:
			if err := add(spec); err != nil {
				return nil, err
			}
		}
	}
	return out, nil
}

func parsePorts(list []string) ([]uint16, error) {
	var out []uint16
	for _, p := range list {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		n, err := strconv.ParseUint(p, 10, 16)
		if err != nil || n == 0 {
			return nil, fmt.Errorf("invalid port %q: use 1-65535", p)
		}
		out = append(out, uint16(n))
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no port given: set --ports")
	}
	return out, nil
}

func discoverLimit(cfg *config.DiscoverConfig) int {
	if cfg.ForceLarge {
		return 0
	}
	return MaxDiscoverHosts
}

// CheckDiscover validates the configuration of `discover`.
func CheckDiscover(cfg *config.DiscoverConfig) error {
	hosts, err := ExpandHosts(cfg.Subnets, discoverLimit(cfg))
	if err != nil {
		return err
	}
	if len(hosts) == 0 {
		return fmt.Errorf("nothing to probe: set --subnets (e.g. --subnets 192.168.1.0/24)")
	}
	if _, err := parsePorts(cfg.Ports); err != nil {
		return err
	}
	if cfg.Parallel == 0 || cfg.Parallel > 256 {
		return fmt.Errorf("invalid --parallel %d: use 1-256", cfg.Parallel)
	}
	return nil
}

// Probe results.
const (
	probeClosed = ""
	probeOpen   = "open"
	probeIEC104 = "iec104"
)

// probe connects to addr and sends a test frame (TESTFR act). A station answers it with
// TESTFR con in any state, so the probe neither starts data transfer nor asks for any
// data: it is the least a controlling station can say.
func probe(ctx context.Context, addr string, timeout time.Duration) (status string, rtt time.Duration) {
	begin := time.Now()
	d := net.Dialer{Timeout: timeout}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return probeClosed, 0
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(timeout))
	frame, _ := apci.NewU(apci.TestFRAct).MarshalBinary()
	if _, err := conn.Write(frame); err != nil {
		return probeOpen, time.Since(begin)
	}
	for range 4 { // a station may send something else first
		f, err := apci.ReadFrame(conn)
		if err != nil {
			return probeOpen, time.Since(begin)
		}
		if f.Format == apci.FormatU && f.Function == apci.TestFRCon {
			return probeIEC104, time.Since(begin)
		}
	}
	return probeOpen, time.Since(begin)
}

// Discover probes every address of the subnets for an IEC 60870-5-104 station. progress
// receives a line per device found.
func Discover(ctx context.Context, cfg *config.DiscoverConfig, progress io.Writer) (*types.DiscoverResult, error) {
	hosts, err := ExpandHosts(cfg.Subnets, discoverLimit(cfg))
	if err != nil {
		return nil, err
	}
	ports, err := parsePorts(cfg.Ports)
	if err != nil {
		return nil, err
	}
	timeout := defaultProbeTimeout
	if cfg.Timeout != 0 {
		timeout = time.Duration(cfg.Timeout) * time.Millisecond
	}

	type job struct {
		host string
		port uint16
	}
	jobs := make(chan job)
	var (
		mu      sync.Mutex
		devices []types.DiscoveredDevice
		wg      sync.WaitGroup
	)
	begin := time.Now()
	for range int(cfg.Parallel) {
		wg.Go(func() {
			for j := range jobs {
				status, rtt := probe(ctx, net.JoinHostPort(j.host, strconv.Itoa(int(j.port))), timeout)
				if status == probeClosed {
					continue
				}
				mu.Lock()
				devices = append(devices, types.DiscoveredDevice{IP: j.host, Port: j.port, Status: status, RTTMs: ms(rtt)})
				if progress != nil && (status == probeIEC104 || cfg.Debug) {
					_, _ = fmt.Fprintf(progress, "discover: %s port %d: %s\n", j.host, j.port, status)
				}
				mu.Unlock()
			}
		})
	}
feed:
	for _, h := range hosts {
		for _, p := range ports {
			select {
			case jobs <- job{h, p}:
			case <-ctx.Done():
				break feed
			}
		}
	}
	close(jobs)
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return nil, WrapError(err)
	}

	sort.Slice(devices, func(i, j int) bool {
		a, b := devices[i], devices[j]
		ai, aerr := netip.ParseAddr(a.IP)
		bi, berr := netip.ParseAddr(b.IP)
		switch {
		case aerr == nil && berr == nil && ai != bi:
			return ai.Less(bi)
		case a.IP != b.IP:
			return a.IP < b.IP
		}
		return a.Port < b.Port
	})
	res := &types.DiscoverResult{Subnets: cfg.Subnets, Ports: ports, Hosts: len(hosts), DurationMs: ms(time.Since(begin)), Devices: []types.DiscoveredDevice{}}
	for _, d := range devices {
		res.Devices = append(res.Devices, d)
		if d.Status == probeIEC104 {
			res.Found++
		}
	}
	return res, nil
}
