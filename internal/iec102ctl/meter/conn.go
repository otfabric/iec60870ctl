// SPDX-License-Identifier: MIT

// Package meter is what the client commands of iec102ctl do: open the line to an integrated
// total station (a meter or data concentrator), start the link, optionally open an REE
// session, run one request and close everything again.
package meter

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/otfabric/go-iec60870-5/ft12"
	"github.com/otfabric/go-iec60870-5/iec102"
	"github.com/otfabric/go-iec60870-5/iec102/client"
	"github.com/otfabric/go-iec60870-5/iec102/ree"
	"github.com/otfabric/go-iec60870-5/link"
	"github.com/otfabric/iec60870ctl/internal/errs"
	"github.com/otfabric/iec60870ctl/internal/iec102ctl/config"
	"github.com/otfabric/iec60870ctl/internal/iec102ctl/types"
	"github.com/otfabric/iec60870ctl/internal/line"
)

// DefaultTimeout bounds connecting, the link start-up and each request when --timeout is 0.
const DefaultTimeout = 30 * time.Second

// Timeout returns the timeout of --timeout ms.
func Timeout(ms uint32) time.Duration {
	if ms == 0 {
		return DefaultTimeout
	}
	return time.Duration(ms) * time.Millisecond
}

// LinkAddrSize returns the library's link address size of --link-address-size.
func LinkAddrSize(n uint8) (int, error) {
	switch n {
	case 0:
		return iec102.NoLinkAddr, nil
	case 1, 2:
		return int(n), nil
	}
	return 0, fmt.Errorf("--link-address-size %d: use 1, 2 or 0 (none)", n)
}

// CheckLinkAddress checks that addr fits a link address field of size octets.
func CheckLinkAddress(addr uint16, size int) error {
	switch {
	case size == iec102.NoLinkAddr && addr != 0:
		return fmt.Errorf("--link-address %d: without a link address field (--link-address-size 0) the address is 0", addr)
	case size == 1 && addr > 254:
		return fmt.Errorf("--link-address %d: 0-254 with one octet (255 is the broadcast address)", addr)
	case size == 2 && addr > 65534:
		return fmt.Errorf("--link-address %d: 0-65534 with two octets (65535 is the broadcast address)", addr)
	}
	return nil
}

// CheckCommonAddress checks that ca fits a common address of size octets.
func CheckCommonAddress(ca uint16, size uint8) error {
	switch size {
	case 1:
		if ca > 255 {
			return fmt.Errorf("--common-address %d: 0-255 with one octet", ca)
		}
	case 2:
	default:
		return fmt.Errorf("--ca-size %d: use 1 or 2", size)
	}
	return nil
}

// Profile of a command: whether the REE profile applies and the access key, if any.
type Profile struct {
	REE    bool
	Key    uint32
	HasKey bool
}

// ResolveProfile checks --profile, --access-key and --access-key-file.
func ResolveProfile(cfg *config.ConnConfig) (Profile, error) {
	var p Profile
	switch strings.ToLower(strings.TrimSpace(cfg.Profile)) {
	case "", config.ProfileStandard:
	case config.ProfileREE:
		p.REE = true
	default:
		return p, fmt.Errorf("invalid --profile %q: use standard or ree", cfg.Profile)
	}
	p.REE = p.REE || cfg.REE
	raw := strings.TrimSpace(cfg.AccessKey)
	if cfg.AccessKeyFile != "" {
		if raw != "" {
			return p, errors.New("--access-key and --access-key-file are mutually exclusive")
		}
		s, err := readKeyFile(cfg.AccessKeyFile)
		if err != nil {
			return p, err
		}
		if s == "" {
			return p, fmt.Errorf("access key file %s is empty", cfg.AccessKeyFile)
		}
		raw = s
	}
	if raw != "" {
		k, err := ParseAccessKey(raw)
		if err != nil {
			return p, err
		}
		p.Key, p.HasKey, p.REE = k, true, true
	}
	return p, nil
}

// Session is an open link to a meter.
type Session struct {
	*client.Client
	Target  line.Target
	Params  iec102.Params
	Timeout time.Duration
	Profile Profile
	Meter   types.Meter
}

// prepared is everything Dial needs, checked before any I/O.
type prepared struct {
	target   line.Target
	settings line.Settings
	cfg      client.Config
	timeout  time.Duration
	profile  Profile
}

func prepare(cfg *config.ConnConfig) (*prepared, error) {
	t, err := line.Resolve(cfg.URL, cfg.IP, cfg.Port, "meter")
	if err != nil {
		return nil, errs.InvalidInput(errs.CodeInvalidURL, err.Error(), err)
	}
	s := line.Settings{Baud: cfg.Baud, DataBits: cfg.DataBits, Parity: cfg.Parity, StopBits: cfg.StopBits}
	if t.Serial() {
		if err := line.CheckSettings(s); err != nil {
			return nil, err
		}
	}
	size, err := LinkAddrSize(cfg.LinkAddressSize)
	if err != nil {
		return nil, err
	}
	if err := CheckLinkAddress(cfg.LinkAddress, size); err != nil {
		return nil, err
	}
	if err := CheckCommonAddress(cfg.CommonAddress, cfg.CASize); err != nil {
		return nil, err
	}
	loc, err := Location(cfg.TimeZone)
	if err != nil {
		return nil, err
	}
	prof, err := ResolveProfile(cfg)
	if err != nil {
		return nil, err
	}
	p := iec102.Params{CommonAddrSize: int(cfg.CASize), Location: loc}
	if prof.REE {
		p.Profile = ree.Profile
	}
	timeout := Timeout(cfg.Timeout)
	cc := client.Config{
		LinkAddrSize: size, LinkAddr: cfg.LinkAddress, CommonAddr: cfg.CommonAddress, Params: p,
		RequestTimeout: timeout,
	}
	cc.Link.Timeout = time.Duration(cfg.LinkTimeout) * time.Millisecond
	cc.Link.Retries = int(cfg.LinkRetries)
	return &prepared{target: t, settings: s, cfg: cc, timeout: timeout, profile: prof}, nil
}

// Validate checks everything about cfg that can be checked without opening the line.
func Validate(cfg *config.ConnConfig) error {
	_, err := prepare(cfg)
	return err
}

// Dial opens the line of cfg and starts the link (request status of link, reset of remote
// link), bounded by the timeout of cfg. trace receives the frame trace of --debug.
func Dial(ctx context.Context, cfg *config.ConnConfig, trace io.Writer) (*Session, error) {
	pr, err := prepare(cfg)
	if err != nil {
		return nil, errs.WrapValidation(err)
	}
	if cfg.Debug && trace != nil {
		tr := &Tracer{W: trace, Params: pr.cfg.Params}
		pr.cfg.Link.OnFrame = tr.Frame
		pr.cfg.Link.OnDiscard = tr.Discard
	}
	cctx, cancel := context.WithTimeout(ctx, pr.timeout)
	defer cancel()
	var c *client.Client
	for attempt := 0; ; attempt++ {
		c, err = open(cctx, pr)
		if err == nil {
			break
		}
		// A line closed during the link start-up is one that is busy: a terminal server or
		// a simulated meter serves one controlling station at a time and closes the next
		// connection at once, also the one that comes just before the previous one has
		// ended. Nothing but the link start-up was sent, so it is tried again, twice.
		if attempt == len(busyRetry) || !errors.Is(err, link.ErrClosed) || cctx.Err() != nil {
			return nil, err
		}
		select {
		case <-time.After(busyRetry[attempt]):
		case <-cctx.Done():
			return nil, err
		}
	}
	return &Session{
		Client: c, Target: pr.target, Params: pr.cfg.Params, Timeout: pr.timeout, Profile: pr.profile,
		Meter: types.Meter{Target: pr.target.String(), LinkAddress: cfg.LinkAddress, CommonAddr: cfg.CommonAddress},
	}, nil
}

// busyRetry are the pauses before a line that was closed during the link start-up is
// opened again.
var busyRetry = []time.Duration{100 * time.Millisecond, 400 * time.Millisecond}

// open opens the line and starts the link. A failure is classified, but keeps its cause.
func open(ctx context.Context, pr *prepared) (*client.Client, error) {
	rw, err := line.Dial(ctx, pr.target, pr.settings, pr.timeout)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return nil, WrapError(err)
		}
		return nil, errs.New(errs.KindTransport, errs.CodeTransportConnectFailed, fmt.Sprintf("connection error: %v", err), err)
	}
	c, err := client.New(rw, pr.cfg)
	if err != nil {
		_ = rw.Close()
		return nil, errs.WrapValidation(err)
	}
	if err := c.Init(ctx); err != nil {
		_ = c.Close()
		return nil, linkStartError(err, pr.cfg.LinkAddr)
	}
	return c, nil
}

// linkStartError classifies a failed link start-up.
func linkStartError(err error, addr uint16) error {
	if errors.Is(err, link.ErrTimeout) || errors.Is(err, context.DeadlineExceeded) {
		return errs.New(errs.KindTimeout, errs.CodeLinkTimeout,
			fmt.Sprintf("timeout: no meter answered the link start-up at link address %d: %v", addr, err), err)
	}
	return WrapError(err)
}

// Run opens the line, runs op and closes it again. With an access key, op runs inside an
// REE session: C_AC_NA_2 before, C_FS_NA_2 after (also when op failed). The result's meter
// carries the session and the duration.
func Run(ctx context.Context, cfg *config.ConnConfig, trace io.Writer, op func(ctx context.Context, s *Session) error) (types.Meter, error) {
	start := time.Now()
	s, err := Dial(ctx, cfg, trace)
	if err != nil {
		return types.Meter{}, err
	}
	defer func() { _ = s.Close() }()
	if s.Profile.HasKey {
		s.Meter.Session = &types.Session{}
		if err := s.login(ctx); err != nil {
			return s.Meter, err
		}
		s.Meter.Session.Login = true
	}
	opErr := op(ctx, s)
	if s.Profile.HasKey {
		lctx, cancel := s.bound(context.WithoutCancel(ctx))
		err := ree.Logout(lctx, s.Client)
		cancel()
		s.Meter.Session.Logout = err == nil
	}
	s.Meter.DurationMs = ms(time.Since(start))
	if opErr != nil {
		return s.Meter, WrapError(opErr)
	}
	return s.Meter, nil
}

// bound returns ctx bounded by the request timeout.
func (s *Session) bound(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, s.Timeout)
}

func (s *Session) login(ctx context.Context) error {
	lctx, cancel := s.bound(ctx)
	defer cancel()
	err := ree.Login(lctx, s.Client, s.Profile.Key)
	if refused, ok := errors.AsType[*iec102.RefusedError](err); ok {
		return errs.New(errs.KindNegative, errs.CodeLoginRefused,
			fmt.Sprintf("the meter refused the access key (C_AC_NA_2 answered with %s)", refused.Cause()), err)
	}
	return WrapError(err)
}

// SessionPartial reports a result whose operation succeeded but whose REE session did not
// end with a confirmed logout: exit code 7.
func SessionPartial(m types.Meter) bool { return m.Session != nil && !m.Session.Logout }

func ms(d time.Duration) float64 { return float64(d.Microseconds()) / 1000 }

// Tracer prints every frame sent and received, for --debug.
type Tracer struct {
	W      io.Writer
	Params iec102.Params
	mu     sync.Mutex
}

// Frame is a link.Config OnFrame hook.
func (t *Tracer) Frame(tx bool, f ft12.Frame) {
	dir := "<-"
	if tx {
		dir = "->"
	}
	text := f.String()
	if f.Kind == ft12.KindVariable {
		text += "  " + DescribeASDU(f.Data, t.Params)
	}
	t.line(dir, text)
}

// Discard is a link.Config OnDiscard hook.
func (t *Tracer) Discard(b []byte, err error) {
	t.line("!!", fmt.Sprintf("%d octets discarded: %v", len(b), err))
}

func (t *Tracer) line(dir, text string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	_, _ = fmt.Fprintf(t.W, "debug: %s %s %s\n", time.Now().UTC().Format("15:04:05.000"), dir, text)
}

// DescribeASDU describes the octets of an ASDU for a trace. The access key of C_AC_NA_2 is
// not shown.
func DescribeASDU(data []byte, p iec102.Params) string {
	a, err := iec102.Decode(data, p)
	if err != nil {
		return fmt.Sprintf("undecodable ASDU: %v  [% x]", err, data)
	}
	name := TypeName(a.Type)
	desc := fmt.Sprintf("%s %s CA=%d record=%d", name, a.Cause, a.CommonAddr, a.Record)
	if a.Type == ree.C_AC_NA_2 {
		return desc + "  [access key hidden]"
	}
	return fmt.Sprintf("%s  [% x]", desc, data)
}
