// SPDX-License-Identifier: MIT

package meter

import (
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/otfabric/go-iec60870-5/iec102"
	"github.com/otfabric/go-iec60870-5/iec102/client"
	"github.com/otfabric/go-iec60870-5/iec102/ree"
	"github.com/otfabric/iec60870ctl/internal/iec102ctl/types"
)

// Location resolves --timezone: utc (default), local or an IANA zone name.
func Location(name string) (*time.Location, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "", "utc":
		return time.UTC, nil
	case "local":
		return time.Local, nil
	}
	loc, err := time.LoadLocation(strings.TrimSpace(name))
	if err != nil {
		return nil, fmt.Errorf("time zone %q: use utc, local or an IANA name such as Europe/Madrid", name)
	}
	return loc, nil
}

// Layouts of the times on the command line and in a model: the wall clock of the meter
// (no zone), as the interop contract writes time a and time b.
const (
	LayoutA = "2006-01-02T15:04"
	LayoutB = "2006-01-02T15:04:05.000"
)

var layoutsA = []string{LayoutA, "2006-01-02 15:04", "2006-01-02"}

var layoutsB = []string{LayoutB, "2006-01-02T15:04:05", "2006-01-02 15:04:05.000", "2006-01-02 15:04:05", LayoutA, "2006-01-02"}

// parseWall parses s with one of layouts in loc, or as RFC 3339 converted to loc.
func parseWall(s string, layouts []string, loc *time.Location) (time.Time, error) {
	s = strings.TrimSpace(s)
	for _, l := range layouts {
		if t, err := time.ParseInLocation(l, s, loc); err == nil {
			return t, nil
		}
	}
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t.In(loc), nil
	}
	return time.Time{}, fmt.Errorf("invalid time %q: use %s, %s or RFC 3339", s, LayoutA, "2006-01-02")
}

// checkYear refuses a time IEC 102 cannot carry: the year has two digits, 2000..2099.
func checkYear(t time.Time) error {
	if y := t.Year(); y < 2000 || y > 2099 {
		return fmt.Errorf("year %d: IEC 102 times carry the years 2000-2099", y)
	}
	return nil
}

// ParseTimeA parses a time a in the zone loc, which also sets the SU bit.
func ParseTimeA(s string, loc *time.Location) (iec102.TimeA, error) {
	t, err := parseWall(s, layoutsA, loc)
	if err != nil {
		return iec102.TimeA{}, err
	}
	if err := checkYear(t); err != nil {
		return iec102.TimeA{}, err
	}
	return iec102.TimeAOf(t, loc), nil
}

// ParseTimeB parses a time b in the zone loc; "now" is now.
func ParseTimeB(s string, loc *time.Location, now time.Time) (iec102.TimeB, error) {
	if strings.EqualFold(strings.TrimSpace(s), "now") || strings.TrimSpace(s) == "" {
		return iec102.TimeBOf(now, loc), nil
	}
	t, err := parseWall(s, layoutsB, loc)
	if err != nil {
		return iec102.TimeB{}, fmt.Errorf("invalid time %q: use now, %s or RFC 3339", s, LayoutB)
	}
	if err := checkYear(t); err != nil {
		return iec102.TimeB{}, err
	}
	return iec102.TimeBOf(t, loc), nil
}

// TextA writes the fields of a time a as YYYY-MM-DDTHH:MM, as received.
func TextA(t iec102.TimeA) string {
	return fmt.Sprintf("%04d-%02d-%02dT%02d:%02d", 2000+int(t.Year), t.Month, t.Day, t.Hour, t.Minute)
}

// TextB writes the fields of a time b as YYYY-MM-DDTHH:MM:SS.mmm, as received.
func TextB(t iec102.TimeB) string {
	return fmt.Sprintf("%s:%02d.%03d", TextA(t.TimeA), t.Second, t.Millisecond)
}

// TimeOfA returns the result form of a time a.
func TimeOfA(t iec102.TimeA) types.Time {
	return types.Time{Time: TextA(t), SU: t.SummerTime, Invalid: t.Invalid}
}

// TimeOfB returns the result form of a time b.
func TimeOfB(t iec102.TimeB) types.Time {
	return types.Time{Time: TextB(t), SU: t.SummerTime, Invalid: t.Invalid}
}

// typeNames maps the mnemonics of the standard and the REE types to their numbers.
var typeNames = func() map[string]iec102.TypeID {
	m := map[string]iec102.TypeID{}
	for t := iec102.TypeID(0); ; t++ {
		if n := ree.Name(t); !strings.HasPrefix(n, "TypeID(") {
			m[n] = t
		}
		if t == 255 {
			return m
		}
	}
}()

// ParseTypeID parses a type identification: a mnemonic (C_RM_NA_2, standard or REE) or a
// number 0-255.
func ParseTypeID(s string) (iec102.TypeID, error) {
	s = strings.TrimSpace(s)
	if t, ok := typeNames[strings.ToUpper(s)]; ok {
		return t, nil
	}
	num := s
	if u := strings.ToUpper(s); strings.HasPrefix(u, "TYPEID(") && strings.HasSuffix(u, ")") {
		num = s[len("TypeID(") : len(s)-1] // the form TypeName writes for an unnamed type
	}
	n, err := strconv.ParseUint(num, 0, 8)
	if err != nil {
		return 0, fmt.Errorf("invalid type %q: use a name such as C_RM_NA_2 or a number 0-255", s)
	}
	return iec102.TypeID(n), nil
}

// TypeName returns the mnemonic of t: the REE name in the private range, the standard one
// below it, or the number.
func TypeName(t iec102.TypeID) string { return ree.Name(t) }

// ParseCause parses --cause: activation, request or a number 0-63.
func ParseCause(s string) (iec102.Cause, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "activation", "act":
		return iec102.CauseActivation, nil
	case "request", "req":
		return iec102.CauseRequest, nil
	case "deactivation":
		return iec102.CauseDeactivation, nil
	}
	n, err := strconv.ParseUint(strings.TrimSpace(s), 0, 8)
	if err != nil || n > 63 {
		return 0, fmt.Errorf("invalid cause %q: use activation, request or a number 0-63", s)
	}
	return iec102.Cause(n), nil
}

// CauseName returns the name of the cause code of c ("activation confirmation").
func CauseName(c iec102.Cause) string {
	s := iec102.Cause(c.Code()).String()
	if i := strings.IndexByte(s, ' '); i >= 0 {
		return s[i+1:]
	}
	return "unknown"
}

// ParseHex parses octets written as hex: "01 02", "01:02", "0x0102" or "0102".
func ParseHex(s string) ([]byte, error) {
	clean := strings.NewReplacer(" ", "", ":", "", "-", "", ",", "", "\t", "").Replace(strings.TrimSpace(s))
	clean = strings.TrimPrefix(strings.TrimPrefix(clean, "0x"), "0X")
	if clean == "" {
		return nil, nil
	}
	b, err := hex.DecodeString(clean)
	if err != nil {
		return nil, fmt.Errorf("invalid --data %q: write the octets as hex, such as \"01 02\"", s)
	}
	return b, nil
}

// Until values of --until.
const (
	UntilAuto         = "auto"
	UntilReply        = "reply"
	UntilConfirmation = "confirmation"
	UntilTermination  = "termination"
)

// UntilValues describes the values of --until.
func UntilValues() map[string]string {
	return map[string]string{
		UntilAuto:         "termination for cause activation, else reply",
		UntilReply:        "the first answer that is not the mirror of the request",
		UntilConfirmation: "the activation confirmation (cause 7)",
		UntilTermination:  "the activation termination (cause 10)",
	}
}

// ParseUntil parses --until for a request of cause c.
func ParseUntil(s string, c iec102.Cause) (client.Until, string, error) {
	v := strings.ToLower(strings.TrimSpace(s))
	if v == "" || v == UntilAuto {
		v = UntilReply
		if c.Code() == iec102.CauseActivation {
			v = UntilTermination
		}
	}
	switch v {
	case UntilReply:
		return client.UntilReply, v, nil
	case UntilConfirmation:
		return client.UntilConfirmation, v, nil
	case UntilTermination:
		return client.UntilTermination, v, nil
	}
	return 0, "", fmt.Errorf("invalid --until %q: use auto, reply, confirmation or termination", s)
}

// ParseAccessKey parses an REE access key: 0-4294967295, decimal or 0x hex.
func ParseAccessKey(s string) (uint32, error) {
	n, err := strconv.ParseUint(strings.TrimSpace(s), 0, 32)
	if err != nil {
		return 0, fmt.Errorf("invalid access key: use a number 0-4294967295 (or 0x hex)")
	}
	return uint32(n), nil
}

// maxKeyFile bounds what is read of --access-key-file.
const maxKeyFile = 256

// readKeyFile returns the access key held in path.
func readKeyFile(path string) (string, error) {
	f, err := os.Open(filepath.Clean(path))
	if err != nil {
		return "", fmt.Errorf("access key file: %w", err)
	}
	defer func() { _ = f.Close() }()
	buf := make([]byte, maxKeyFile+1)
	n, _ := f.Read(buf)
	if n > maxKeyFile {
		return "", fmt.Errorf("access key file %s: more than %d octets", path, maxKeyFile)
	}
	return strings.TrimSpace(string(buf[:n])), nil
}

// Kinds of integrated totals (--kind) and the read commands of each: oldest period, oldest
// with an address range, a past period, a past period with an address range, a time range
// (Beckhoff TS650x manual, sections 4.22.10 to 4.22.29).
var kindReads = map[string][5]iec102.TypeID{
	"accounting":        {iec102.C_CI_NA_2, iec102.C_CI_NB_2, iec102.C_CI_NC_2, iec102.C_CI_ND_2, iec102.C_CI_NR_2},
	"reset-accounting":  {iec102.C_CI_NE_2, iec102.C_CI_NF_2, iec102.C_CI_NG_2, iec102.C_CI_NH_2, iec102.C_CI_NS_2},
	"operational":       {iec102.C_CI_NI_2, iec102.C_CI_NK_2, iec102.C_CI_NL_2, iec102.C_CI_NM_2, iec102.C_CI_NT_2},
	"reset-operational": {iec102.C_CI_NN_2, iec102.C_CI_NO_2, iec102.C_CI_NP_2, iec102.C_CI_NQ_2, iec102.C_CI_NU_2},
}

// kindAliases are the names of the interop contract.
var kindAliases = map[string]string{"absolute": "operational", "incremental": "reset-operational"}

// KindValues describes the values of --kind.
func KindValues() map[string]string {
	return map[string]string{
		"operational":       "operational integrated totals (C_CI_NI_2..NM_2, NT_2; M_IT_TG_2)",
		"reset-operational": "periodical reset operational integrated totals (C_CI_NN_2..NQ_2, NU_2; M_IT_TK_2)",
		"accounting":        "accounting integrated totals (C_CI_NA_2..ND_2, NR_2; M_IT_TA_2)",
		"reset-accounting":  "periodical reset accounting integrated totals (C_CI_NE_2..NH_2, NS_2; M_IT_TD_2)",
		"absolute":          "operational",
		"incremental":       "reset-operational",
	}
}

// KindName returns the --kind name of a family.
func KindName(f iec102.Family) string {
	switch f {
	case iec102.FamilyAccounting:
		return "accounting"
	case iec102.FamilyResetAccounting:
		return "reset-accounting"
	case iec102.FamilyOperational:
		return "operational"
	case iec102.FamilyResetOperational:
		return "reset-operational"
	default:
		return "none"
	}
}

// QualityFlags names the set bits of a quality descriptor: IV, CA, CY and the sequence
// number of the standard, or IV, CA, CY, VH, MP, IN, AL of the REE profile. "good" when
// none is set.
func QualityFlags(q iec102.Quality, reeProfile bool) string {
	var f []string
	for _, b := range []struct {
		bit  iec102.Quality
		name string
	}{{0x80, "IV"}, {0x40, "CA"}, {0x20, "CY"}} {
		if q&b.bit != 0 {
			f = append(f, b.name)
		}
	}
	if reeProfile {
		for _, b := range []struct {
			bit  ree.Quality
			name string
		}{{ree.QualityVH, "VH"}, {ree.QualityMP, "MP"}, {ree.QualityIN, "IN"}, {ree.QualityAL, "AL"}, {0x01, "RES"}} {
			if ree.Quality(q).Has(b.bit) {
				f = append(f, b.name)
			}
		}
	} else if s := q.Seq(); s != 0 {
		f = append(f, "SEQ="+strconv.Itoa(int(s)))
	}
	if len(f) == 0 {
		return "good"
	}
	return strings.Join(f, "|")
}
