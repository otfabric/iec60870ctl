// SPDX-License-Identifier: MIT

// Package config holds the configuration of every iec102ctl command: struct fields bound to
// flags and to IEC102CTL_* environment variables (internal/envflag).
package config

import "github.com/otfabric/iec60870ctl/internal/envflag"

// The environment and flag binding is shared by every tool of the repository
// (internal/envflag).
var (
	LoadFromEnv     = envflag.LoadFromEnv
	MustLoadFromEnv = envflag.MustLoadFromEnv
	RegisterFlags   = envflag.RegisterFlags
	Getenv          = envflag.Getenv
)

// Defaults of the link flags: those of the IEC 102 link section of the interop contract of
// otfabric/iec60870-5-interop (two octets of link and common address, both addresses 1).
const (
	DefaultLinkAddress     = 1
	DefaultLinkAddressSize = 2
	DefaultCommonAddress   = 1
	DefaultCASize          = 2
)

// Profiles of --profile.
const (
	ProfileStandard = "standard"
	ProfileREE      = "ree"
)

// LineConfig is the line of a controlling station: a terminal server over TCP or a serial
// port (internal/line).
type LineConfig struct {
	URL      string `env:"IEC102CTL_URL" desc:"Line: tcp://host[:port] (a terminal server; port 2401 by default) or serial:///dev/ttyUSB0 (serial://COM3); mutually exclusive with --ip/--port" flag:"url"`
	IP       string `env:"IEC102CTL_IP" desc:"Terminal server IP address or host name (used when --url is not set)" flag:"ip"`
	Port     uint16 `env:"IEC102CTL_PORT" desc:"Terminal server TCP port (used when --url is not set; 0 = 2401)" flag:"port"`
	Baud     uint32 `env:"IEC102CTL_BAUD" desc:"Serial line: bit rate (0 = 9600)" flag:"baud"`
	DataBits uint8  `env:"IEC102CTL_DATA_BITS" desc:"Serial line: data bits (0 = 8)" flag:"data-bits"`
	Parity   string `env:"IEC102CTL_PARITY" desc:"Serial line: parity even, odd or none (FT 1.2: even)" flag:"parity"`
	StopBits uint8  `env:"IEC102CTL_STOP_BITS" desc:"Serial line: stop bits (0 = 1)" flag:"stop-bits"`
}

// LinkConfig holds the link and ASDU parameters both ends of a line must agree on.
type LinkConfig struct {
	LinkAddress     uint16 `env:"IEC102CTL_LINK_ADDRESS" desc:"Link address of the meter: 0-65534 with two octets, 0-254 with one" flag:"link-address"`
	LinkAddressSize uint8  `env:"IEC102CTL_LINK_ADDRESS_SIZE" desc:"Octets of the link address field: 1 or 2, or 0 for none (point-to-point)" flag:"link-address-size"`
	CommonAddress   uint16 `env:"IEC102CTL_COMMON_ADDRESS" desc:"Common address of ASDU: the measuring point address" flag:"common-address"`
	CASize          uint8  `env:"IEC102CTL_CA_SIZE" desc:"Octets of the common address: 1 or 2" flag:"ca-size"`
	TimeZone        string `env:"IEC102CTL_TIMEZONE" desc:"Time zone of the meter's clock, in which times without a zone are read and the SU bit is set: utc, local or an IANA name such as Europe/Madrid (default utc)" flag:"timezone"`
	LinkTimeout     uint32 `env:"IEC102CTL_LINK_TIMEOUT" desc:"Milliseconds to wait for the answer to a frame before repeating it (0 = 1000)" flag:"link-timeout"`
	LinkRetries     uint8  `env:"IEC102CTL_LINK_RETRIES" desc:"How often an unanswered frame is repeated, with the same FCB (0 = 3)" flag:"link-retries"`
}

// SessionConfig selects the REE profile and its session.
type SessionConfig struct {
	Profile       string `env:"IEC102CTL_PROFILE" desc:"Profile: standard, or ree (the Spanish REE profile: decode its types, and with an access key a session around the operation)" flag:"profile"`
	AccessKey     string `env:"IEC102CTL_ACCESS_KEY" desc:"REE access key of the measuring point, 0-4294967295 (or 0x hex): opens a session (C_AC_NA_2) before and closes it (C_FS_NA_2) after the operation; implies --profile ree" flag:"access-key"`
	AccessKeyFile string `env:"IEC102CTL_ACCESS_KEY_FILE" desc:"File holding the REE access key (instead of --access-key)" flag:"access-key-file"`
}

// ConnConfig is what every client command needs to reach a meter.
type ConnConfig struct {
	LineConfig
	LinkConfig
	SessionConfig
	Timeout uint32 `env:"IEC102CTL_TIMEOUT" desc:"Milliseconds for connecting, starting the link and each request (0 = 30000; long load profiles need more)" flag:"timeout"`
	Debug   bool   // set from global --debug (root persistent flag)
	// REE is set by the commands of `client ree`: the REE profile whatever --profile says.
	REE bool
}

// Defaults returns the connection defaults of every client command.
func Defaults() ConnConfig {
	return ConnConfig{
		LinkConfig: LinkConfig{
			LinkAddress: DefaultLinkAddress, LinkAddressSize: DefaultLinkAddressSize,
			CommonAddress: DefaultCommonAddress, CASize: DefaultCASize,
		},
		SessionConfig: SessionConfig{Profile: ProfileStandard},
	}
}

// InfoConfig drives `client info`.
type InfoConfig struct {
	ConnConfig
	OutputFormat string `env:"IEC102CTL_FORMAT" desc:"Output format: text, json, or table" flag:"format"`
}

// TimeConfig drives `client time`.
type TimeConfig struct {
	ConnConfig
	OutputFormat string `env:"IEC102CTL_FORMAT" desc:"Output format: text, json, or table" flag:"format"`
}

// TotalsConfig drives `client totals`.
type TotalsConfig struct {
	ConnConfig
	Record uint8  `env:"IEC102CTL_RECORD" desc:"Record address: 11 (period 1, the hourly load profile of REE), 12 (period 2, quarter hours), 21 (daily values), 1 (accounting period), ..." flag:"record"`
	Kind   string `env:"IEC102CTL_KIND" desc:"Integrated totals: operational (absolute, the default), reset-operational (incremental), accounting or reset-accounting" flag:"kind"`
	From   string `env:"IEC102CTL_FROM" desc:"Start of the time range (time a, the meter's local time): 2026-01-31T00:00, 2026-01-31, or RFC 3339; needs --to" flag:"from"`
	To     string `env:"IEC102CTL_TO" desc:"End of the time range (inclusive); needs --from" flag:"to"`
	Period string `env:"IEC102CTL_PERIOD" desc:"One past integration period, by the time of its end (instead of --from/--to)" flag:"period"`
	Oldest bool   `env:"IEC102CTL_OLDEST" desc:"The oldest integration period the meter holds (instead of --from/--to)" flag:"oldest"`
	First  uint8  `env:"IEC102CTL_FIRST" desc:"First integrated total address of the range" flag:"first"`
	Last   uint8  `env:"IEC102CTL_LAST" desc:"Last integrated total address of the range" flag:"last"`
	Type   string `env:"IEC102CTL_READ_TYPE" desc:"The read command itself, C_CI_NA_2 .. C_CI_NU_2 (or 104-123), instead of --kind and the selection" flag:"type"`
	// AddressRange is set when --first or --last was given: --period and --oldest then
	// select a range of addresses too.
	AddressRange bool
	OutputFormat string `env:"IEC102CTL_FORMAT" desc:"Output format: text, json, or table" flag:"format"`
}

// EventsConfig drives `client events`.
type EventsConfig struct {
	ConnConfig
	Record       uint8  `env:"IEC102CTL_RECORD" desc:"Record address of the list: 51 (complete list), 50 (oldest message), 52-55 (partial lists; REE 52: power on and voltage)" flag:"record"`
	From         string `env:"IEC102CTL_FROM" desc:"Start of the time range (time a); needs --to. Without both the whole list is read" flag:"from"`
	To           string `env:"IEC102CTL_TO" desc:"End of the time range (inclusive); needs --from" flag:"to"`
	OutputFormat string `env:"IEC102CTL_FORMAT" desc:"Output format: text, json, or table" flag:"format"`
}

// RequestConfig drives `client request`.
type RequestConfig struct {
	ConnConfig
	Type         string `env:"IEC102CTL_REQUEST_TYPE" desc:"Type identification of the request: a name (C_RM_NA_2) or a number 0-255" flag:"type"`
	Cause        string `env:"IEC102CTL_CAUSE" desc:"Cause of transmission: activation (6), request (5) or a number 0-63" flag:"cause"`
	Record       uint8  `env:"IEC102CTL_RECORD" desc:"Record address" flag:"record"`
	Data         string `env:"IEC102CTL_DATA" desc:"Information objects after the record address, as hex (\"01 02\", 0x0102); empty for none" flag:"data"`
	Objects      int    `env:"IEC102CTL_OBJECTS" desc:"Number of information objects in the VSQ, 0-127 (-1 = 0 without --data, else 1)" flag:"objects"`
	Until        string `env:"IEC102CTL_UNTIL" desc:"When the request is complete: reply (the first answer that is not the mirror), confirmation (cause 7), termination (cause 10) or auto (termination for cause activation, else reply)" flag:"until"`
	OutputFormat string `env:"IEC102CTL_FORMAT" desc:"Output format: text, json, or table" flag:"format"`
}

// SessionOnlyConfig drives `client ree login`.
type SessionOnlyConfig struct {
	ConnConfig
	OutputFormat string `env:"IEC102CTL_FORMAT" desc:"Output format: text, json, or table" flag:"format"`
}

// SetTimeConfig drives `client ree set-time`.
type SetTimeConfig struct {
	ConnConfig
	Time         string `env:"IEC102CTL_TIME" desc:"Time to set: now, a time b of the meter's local time (2026-01-31T12:00:00.000) or RFC 3339" flag:"time"`
	OutputFormat string `env:"IEC102CTL_FORMAT" desc:"Output format: text, json, or table" flag:"format"`
}

// TariffsConfig drives `client ree tariffs`.
type TariffsConfig struct {
	ConnConfig
	Register     uint8  `env:"IEC102CTL_REGISTER" desc:"Contract register: 134-136 (contracts I-III), 137-139 (the latent ones)" flag:"register"`
	From         string `env:"IEC102CTL_FROM" desc:"Stored values (C_TA_VM_2) of the billing periods that end in this range (time a); needs --to. Without both: current values (C_TA_VC_2)" flag:"from"`
	To           string `env:"IEC102CTL_TO" desc:"End of the range (inclusive); needs --from" flag:"to"`
	OutputFormat string `env:"IEC102CTL_FORMAT" desc:"Output format: text, json, or table" flag:"format"`
}

// REEReadConfig drives `client ree contracted-powers` and `client ree special-days`.
type REEReadConfig struct {
	ConnConfig
	Record       uint8  `env:"IEC102CTL_RECORD" desc:"Record address of the request" flag:"record"`
	OutputFormat string `env:"IEC102CTL_FORMAT" desc:"Output format: text, json, or table" flag:"format"`
}

// TypesConfig drives `client types`.
type TypesConfig struct {
	Direction    string `env:"IEC102CTL_DIRECTION" desc:"Only list types of this direction: monitor or control" flag:"direction"`
	Profile      string `env:"IEC102CTL_PROFILE" desc:"Only list types of this profile: standard or ree" flag:"profile"`
	OutputFormat string `env:"IEC102CTL_FORMAT" desc:"Output format: text, json, or table" flag:"format"`
}

// ServerConfig drives `server start`.
type ServerConfig struct {
	Config          string `env:"IEC102CTL_CONFIG" desc:"Meter model file (YAML or JSON); without it a built-in demo meter is served" flag:"config"`
	Listen          string `env:"IEC102CTL_LISTEN" desc:"Address to listen on" flag:"listen"`
	Port            uint16 `env:"IEC102CTL_PORT" desc:"TCP port to listen on (0 = 2401)" flag:"port"`
	Serial          string `env:"IEC102CTL_SERIAL" desc:"Serve a serial port (/dev/ttyUSB0, COM3) instead of listening on TCP" flag:"serial"`
	Baud            uint32 `env:"IEC102CTL_BAUD" desc:"Serial line: bit rate (0 = 9600)" flag:"baud"`
	DataBits        uint8  `env:"IEC102CTL_DATA_BITS" desc:"Serial line: data bits (0 = 8)" flag:"data-bits"`
	Parity          string `env:"IEC102CTL_PARITY" desc:"Serial line: parity even, odd or none (FT 1.2: even)" flag:"parity"`
	StopBits        uint8  `env:"IEC102CTL_STOP_BITS" desc:"Serial line: stop bits (0 = 1)" flag:"stop-bits"`
	LinkAddress     int32  `env:"IEC102CTL_LINK_ADDRESS" desc:"Link address of the meter (-1 = the model's)" flag:"link-address"`
	LinkAddressSize uint8  `env:"IEC102CTL_LINK_ADDRESS_SIZE" desc:"Octets of the link address field: 1 or 2, or 0 for none (point-to-point)" flag:"link-address-size"`
	CommonAddress   int32  `env:"IEC102CTL_COMMON_ADDRESS" desc:"Common address of ASDU, the measuring point address (-1 = the model's)" flag:"common-address"`
	CASize          uint8  `env:"IEC102CTL_CA_SIZE" desc:"Octets of the common address: 1 or 2" flag:"ca-size"`
	TimeZone        string `env:"IEC102CTL_TIMEZONE" desc:"Time zone of the meter's clock and of the times in the model: utc, local or an IANA name (default utc)" flag:"timezone"`
	LinkTimeout     uint32 `env:"IEC102CTL_LINK_TIMEOUT" desc:"Milliseconds a frame may take on the line before its octets are discarded (0 = 1000)" flag:"link-timeout"`
	Duration        uint32 `env:"IEC102CTL_DURATION" desc:"Stop after this many seconds (0 = until interrupted)" flag:"duration"`
	OutputFormat    string `env:"IEC102CTL_FORMAT" desc:"Format of the event log on stdout: text, json, or table" flag:"format"`
	Debug           bool   // set from global --debug (root persistent flag)
}

// ServerDefaults returns the defaults of `server start`.
func ServerDefaults() ServerConfig {
	return ServerConfig{LinkAddress: -1, CommonAddress: -1, LinkAddressSize: DefaultLinkAddressSize, CASize: DefaultCASize, OutputFormat: "text"}
}

// GenerateConfigConfig drives `server generate-config`.
type GenerateConfigConfig struct {
	Output string `env:"IEC102CTL_OUTPUT" desc:"File to write the model to (- = stdout)" flag:"output"`
	Format string `env:"IEC102CTL_CONFIG_FORMAT" desc:"Format of the model: yaml or json" flag:"format"`
	Force  bool   `env:"IEC102CTL_FORCE" desc:"Overwrite an existing file" flag:"force"`
}
