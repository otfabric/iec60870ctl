// SPDX-License-Identifier: MIT

package cmd

import (
	"context"
	"crypto/x509"
	"crypto/x509/pkix"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/otfabric/go-iec104/sec"
	"github.com/otfabric/iec104ctl/internal/iec"
	"github.com/otfabric/iec104ctl/internal/station"
	"github.com/otfabric/iec104ctl/internal/stationtest"
)

// secrets are the identities of the security tests, as PEM files in a temporary directory.
type secrets struct {
	dir string
	ca  *sec.Identity
	// Signed by ca.
	station, scada *sec.Identity
	// Self-signed, trusted by pin; rogue is trusted by nobody.
	pinStation, pinScada, rogue *sec.Identity
}

func newSecrets(t testing.TB) *secrets {
	t.Helper()
	id := func(name string, o sec.IdentityOptions) *sec.Identity {
		o.Subject = pkix.Name{CommonName: name}
		i, err := sec.NewIdentity(o)
		if err != nil {
			t.Fatalf("identity %s: %v", name, err)
		}
		return i
	}
	s := &secrets{dir: t.TempDir()}
	s.ca = id("iec104ctl test authority", sec.IdentityOptions{CA: true})
	s.station = id("station", sec.IdentityOptions{Issuer: s.ca})
	s.scada = id("scada", sec.IdentityOptions{Issuer: s.ca})
	s.pinStation = id("pinned station", sec.IdentityOptions{})
	s.pinScada = id("pinned scada", sec.IdentityOptions{})
	s.rogue = id("rogue", sec.IdentityOptions{})
	for name, i := range map[string]*sec.Identity{"ca": s.ca, "station": s.station, "scada": s.scada,
		"pin-station": s.pinStation, "pin-scada": s.pinScada, "rogue": s.rogue} {
		cert, key, err := i.PEM()
		if err != nil {
			t.Fatal(err)
		}
		_ = os.WriteFile(filepath.Join(s.dir, name+".pem"), cert, 0o600)
		_ = os.WriteFile(filepath.Join(s.dir, name+".key"), key, 0o600)
	}
	return s
}

func (s *secrets) path(file string) string { return filepath.Join(s.dir, file) }

// flags are the client security flags for the identity name, trusting the file trust.
func (s *secrets) flags(name, trust string) []string {
	return []string{"--secure", "--security-cert", s.path(name + ".pem"), "--security-key", s.path(name + ".key"),
		"--security-trust", s.path(trust + ".pem")}
}

func (s *secrets) authority() sec.Trust {
	return sec.Trust{Authorities: []*x509.Certificate{s.ca.Certificate}}
}

// secureStation starts the test station with security.
func secureStation(t testing.TB, id *sec.Identity, trust sec.Trust, ais uint16, roles ...sec.AccessRole) *stationtest.Station {
	t.Helper()
	return stationtest.Start(t, stationtest.Model, func(o *station.Options) {
		o.Security = &iec.Security{Identity: id, Trust: trust, ID: ais, Roles: roles}
	})
}

func TestSecureClient(t *testing.T) {
	s := newSecrets(t)

	t.Run("authority, HMAC-SHA-256", func(t *testing.T) {
		st := secureStation(t, s.station, s.authority(), 1)
		r := cli(t, st, append([]string{"interrogate", "--debug"}, s.flags("scada", "ca")...)...).want(t, 0)
		r.has(t, "8 objects", "230.5")
		r.errHas(t, "security: secure session with \"station\", AIM 1, AIS 1, MAC HMAC-SHA-256/16, data HMAC-SHA-256/16", "STAS_PROC_SUCC", "SKEY_PROC_SUCC")
		if !st.HasEvent("secured") {
			t.Errorf("no secured event: %+v", st.Events())
		}
		// Every client command speaks security: one more, through another code path.
		cli(t, st, append([]string{"read", "--ioa", "1000"}, s.flags("scada", "ca")...)...).want(t, 0).has(t, "230.5")
		cli(t, st, append([]string{"read", "--ioa", "1000", "--mal", "hmac-sha3-256"}, s.flags("scada", "ca")...)...).want(t, 0)
	})

	t.Run("pinned, AES-256-GCM", func(t *testing.T) {
		st := secureStation(t, s.pinStation, s.pinScada.Pin(), 9)
		args := append([]string{"interrogate", "--debug", "--association-id", "4711", "--dpa", "aes-256-gcm"}, s.flags("pin-scada", "pin-station")...)
		cli(t, st, args...).want(t, 0).has(t, "8 objects").
			errHas(t, "AIM 4711, AIS 9", "data AEAD-AES-256-GCM")
		cli(t, st, append([]string{"read", "--ioa", "100", "--mal", "3", "--dpa", "11"}, s.flags("pin-scada", "pin-station")...)...).want(t, 0)
	})

	t.Run("roles", func(t *testing.T) {
		viewer := secureStation(t, s.station, s.authority(), 1, sec.RoleViewer)
		cmd := append([]string{"command", "--type", "single", "--ioa", "5000", "--value", "on"}, s.flags("scada", "ca")...)
		kind, code := cli(t, viewer, append(cmd, "--format", "json")...).want(t, 6).errorCode(t)
		if kind != "negative_confirmation" || code != "negative_confirmation" {
			t.Errorf("a viewer's command: %s/%s", kind, code)
		}
		cli(t, viewer, cmd...).want(t, 6).errHas(t, "not-authorized")
		eventually(t, "the refusal to be logged", func() bool { return viewer.HasEvent("refused") })
		// A viewer may still look.
		cli(t, viewer, append([]string{"interrogate"}, s.flags("scada", "ca")...)...).want(t, 0)

		operator := secureStation(t, s.station, s.authority(), 1, sec.RoleViewer, sec.RoleOperator)
		cli(t, operator, cmd...).want(t, 0).has(t, "confirmed")
	})

	t.Run("untrusted", func(t *testing.T) {
		st := secureStation(t, s.station, s.authority(), 1)
		// The station does not trust the client.
		kind, code := cli(t, st, append([]string{"interrogate", "--timeout", "1500", "--format", "json"}, s.flags("rogue", "ca")...)...).
			want(t, 4).errorCode(t)
		if kind != "transport" || code != "security_failed" {
			t.Errorf("an untrusted client: %s/%s", kind, code)
		}
		eventually(t, "the refusal to be logged", func() bool { return st.HasEvent("security-refused") })
		// The client does not trust the station.
		cli(t, st, append([]string{"interrogate", "--timeout", "1500"}, s.flags("scada", "pin-station")...)...).
			want(t, 4).errHas(t, "security error", "certificate refused")
	})

	t.Run("client without security", func(t *testing.T) {
		st := secureStation(t, s.station, s.authority(), 1)
		// Not through run: a context with a deadline would replace --timeout as the bound
		// of the request, as it does for the library.
		var stdout, stderr syncBuffer
		args := append(append([]string{"client", "interrogate"}, target(st)...), "--timeout", "1000", "--format", "json")
		r := result{code: Run(context.Background(), args, &stdout, &stderr)}
		r.stdout, r.stderr = stdout.String(), stderr.String()
		kind, code := r.want(t, 5).errorCode(t)
		if kind != "timeout" || code != "request_timeout" {
			t.Errorf("a client without security: %s/%s", kind, code)
		}
	})

	t.Run("security against a station without it", func(t *testing.T) {
		st := stationtest.Start(t, stationtest.Model)
		kind, code := cli(t, st, append([]string{"interrogate", "--timeout", "1500", "--format", "json"}, s.flags("scada", "ca")...)...).
			want(t, 4).errorCode(t)
		if code != "security_failed" {
			t.Errorf("security against a plain station: %s/%s", kind, code)
		}
	})

	t.Run("invalid", func(t *testing.T) {
		st := stationtest.Start(t, stationtest.Model)
		bad := s.path("bad.pem")
		_ = os.WriteFile(bad, []byte("not PEM"), 0o600)
		for _, args := range [][]string{
			{"--security-cert", s.path("scada.pem")},
			{"--security-trust", s.path("ca.pem")},
			{"--association-id", "5"},
			{"--mal", "4"},
			{"--dpa", "aes-256-gcm"},
			{"--secure"},
			{"--secure", "--security-cert", s.path("scada.pem"), "--security-key", s.path("scada.key")},
			append(s.flags("scada", "ca"), "--mal", "md5"),
			append(s.flags("scada", "ca"), "--mal", "11"),
			append(s.flags("scada", "ca"), "--dpa", "99"),
			append(s.flags("scada", "ca"), "--association-id", "0"),
			s.flags("scada", "station"), // a station certificate that is neither a CA nor self-signed
			s.flags("scada", "bad"),
			s.flags("scada", "missing"),
			{"--secure", "--security-cert", s.path("scada.pem"), "--security-key", s.path("rogue.key"), "--security-trust", s.path("ca.pem")},
			{"--secure", "--security-cert", bad, "--security-key", s.path("scada.key"), "--security-trust", s.path("ca.pem")},
		} {
			r := cli(t, st, append([]string{"interrogate"}, args...)...)
			if r.code != 3 {
				t.Errorf("%s: exit code %d, want 3\n%s", strings.Join(args, " "), r.code, r.stderr)
			}
		}
		// Nothing was sent: the station never saw a connection.
		if st.HasEvent("connected") {
			t.Error("an invalid invocation connected")
		}
		kind, code := cli(t, st, "read", "--ioa", "1", "--mal", "4", "--format", "json").want(t, 3).errorCode(t)
		if kind != "invalid_input" || code != "invalid_flag_combination" {
			t.Errorf("--mal without --secure: %s/%s", kind, code)
		}
	})
}

func TestSecureServer(t *testing.T) {
	s := newSecrets(t)
	port := strconv.Itoa(int(stationtest.FreePort(t)))
	ctx, stop := context.WithCancel(context.Background())
	done := make(chan result, 1)
	go func() {
		done <- runCtx(t, ctx, "server", "start", "--listen", "127.0.0.1", "--port", port, "--format", "json",
			"--secure", "--security-cert", s.path("station.pem"), "--security-key", s.path("station.key"),
			"--security-trust", s.path("ca.pem"), "--association-id", "3", "--roles", "viewer")
	}()
	eventually(t, "the server to listen", func() bool {
		conn, err := net.DialTimeout("tcp", "127.0.0.1:"+port, 200*time.Millisecond)
		if err == nil {
			_ = conn.Close()
		}
		return err == nil
	})
	target := []string{"--ip", "127.0.0.1", "--port", port, "--t1", "2", "--t2", "1"}
	client := func(args ...string) result {
		full := append([]string{"client"}, args[0])
		full = append(full, target...)
		full = append(full, args[1:]...)
		return run(t, append(full, s.flags("scada", "ca")...)...)
	}
	client("ping", "--method", "command", "--count", "1").want(t, 0)
	client("command", "--type", "single", "--ioa", "5000", "--value", "on").want(t, 6)
	run(t, append([]string{"client", "ping", "--count", "1", "--timeout", "1500"}, append(target, s.flags("rogue", "ca")...)...)...).want(t, 4)
	stop()
	r := (<-done).want(t, 0)
	events := map[string]string{}
	for _, e := range r.lines(t) {
		ev, _ := e["event"].(string)
		detail, _ := e["detail"].(string)
		if _, seen := events[ev]; !seen {
			events[ev] = detail
		}
	}
	if !strings.Contains(events["listening"], "secure (AIS 3)") {
		t.Errorf("listening: %q", events["listening"])
	}
	if !strings.Contains(events["secured"], "AIM 1, \"scada\", AIS 3") {
		t.Errorf("secured: %q", events["secured"])
	}
	if !strings.Contains(events["refused"], "not permitted") {
		t.Errorf("refused: %q", events["refused"])
	}
	if !strings.Contains(events["security-refused"], "certificate") {
		t.Errorf("security-refused: %q", events["security-refused"])
	}

	t.Run("invalid", func(t *testing.T) {
		key := []string{"--security-cert", s.path("station.pem"), "--security-key", s.path("station.key"), "--security-trust", s.path("ca.pem")}
		for _, args := range [][]string{
			{"--roles", "viewer"},
			{"--association-id", "2"},
			key,
			{"--secure"},
			append(append([]string{"--secure"}, key...), "--roles", "superuser"),
			append(append([]string{"--secure"}, key...), "--association-id", "0"),
			{"--secure", "--security-cert", s.path("station.pem"), "--security-key", s.path("scada.key"), "--security-trust", s.path("ca.pem")},
			{"--secure", "--security-cert", s.path("station.pem"), "--security-key", s.path("station.key"), "--security-trust", s.path("nothing.pem")},
		} {
			r := run(t, append([]string{"server", "start", "--listen", "127.0.0.1", "--port", port}, args...)...)
			if r.code != 3 {
				t.Errorf("%s: exit code %d, want 3\n%s", strings.Join(args, " "), r.code, r.stderr)
			}
		}
	})
}
