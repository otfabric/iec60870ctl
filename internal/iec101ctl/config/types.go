// SPDX-License-Identifier: MIT

// Package config holds the configuration of every iec101ctl command: struct fields bound
// to flags and to IEC101CTL_* environment variables.
package config

// Link modes of --link-mode.
const (
	ModeUnbalanced = "unbalanced"
	ModeBalanced   = "balanced"
)

// LineConfig holds the character format of a serial line (serial:// URLs and
// --serial). Zero selects the FT 1.2 value. On a TCP line it does not apply: the terminal
// server sets its own line.
type LineConfig struct {
	Baud     uint32 `env:"IEC101CTL_BAUD" desc:"Serial line: baud rate (0 = 9600)" flag:"baud"`
	DataBits uint8  `env:"IEC101CTL_DATA_BITS" desc:"Serial line: data bits (0 = 8)" flag:"data-bits"`
	Parity   string `env:"IEC101CTL_PARITY" desc:"Serial line: parity, even, odd or none (FT 1.2 uses even)" flag:"parity"`
	StopBits uint8  `env:"IEC101CTL_STOP_BITS" desc:"Serial line: stop bits, 1 or 2 (0 = 1)" flag:"stop-bits"`
}

// LinkConfig holds the system parameters of IEC 60870-5-101 that both stations of a line
// must agree on: the link mode, the link addresses, the field sizes and the link timing.
// The flag names are those of the iec60870-5-interop container contract.
type LinkConfig struct {
	Mode           string `env:"IEC101CTL_LINK_MODE" desc:"Link transmission: unbalanced (the controlling station polls) or balanced (both stations send)" flag:"link-mode"`
	LinkAddr       uint16 `env:"IEC101CTL_LINK_ADDRESS" desc:"Link address of the controlled station" flag:"link-address"`
	RemoteLinkAddr string `env:"IEC101CTL_REMOTE_LINK_ADDRESS" desc:"Balanced only: link address of the controlling station (default: --link-address)" flag:"remote-link-address"`
	LinkAddrSize   uint8  `env:"IEC101CTL_LINK_ADDRESS_SIZE" desc:"Octets of the link address field: 1 or 2, or 0 (no address) in balanced mode" flag:"link-address-size"`
	CauseSize      uint8  `env:"IEC101CTL_COT_SIZE" desc:"Octets of the cause of transmission: 1, or 2 with the originator address" flag:"cot-size"`
	CASize         uint8  `env:"IEC101CTL_CA_SIZE" desc:"Octets of the common address: 1 or 2" flag:"ca-size"`
	IOASize        uint8  `env:"IEC101CTL_IOA_SIZE" desc:"Octets of the information object address: 1, 2 or 3" flag:"ioa-size"`
	LinkTimeout    uint32 `env:"IEC101CTL_LINK_TIMEOUT" desc:"Link layer: milliseconds to wait for the answer to a frame before it is repeated (0 = 1000)" flag:"link-timeout"`
	LinkRetries    uint8  `env:"IEC101CTL_LINK_RETRIES" desc:"Link layer: repetitions of an unanswered frame (0 = 3)" flag:"link-retries"`
	Strict         bool   `env:"IEC101CTL_STRICT" desc:"Apply the IEC 60870-5-101 profile checks to every ASDU sent and received" flag:"strict"`
	TimeZone       string `env:"IEC101CTL_TIMEZONE" desc:"Time zone of the time tags on the wire: utc, local or an IANA name such as Europe/Brussels (default utc)" flag:"timezone"`
}

// DefaultLink returns the defaults of the link flags: unbalanced, link address 1 in one
// octet, cause of transmission 1, common address 1 and information object address 2
// octets (the defaults of the iec60870-5-interop container contract).
func DefaultLink() LinkConfig {
	return LinkConfig{Mode: ModeUnbalanced, LinkAddr: 1, LinkAddrSize: 1, CauseSize: 1, CASize: 1, IOASize: 2}
}

// ConnConfig is what every client command needs to reach a station.
type ConnConfig struct {
	LineConfig
	LinkConfig
	URL          string `env:"IEC101CTL_URL" desc:"Line URL: tcp://host[:port] (a terminal server; port 2401 by default) or serial:///dev/ttyUSB0; mutually exclusive with --ip/--port" flag:"url"`
	IP           string `env:"IEC101CTL_IP" desc:"Terminal server IP address or host name (used when --url is not set)" flag:"ip"`
	Port         uint16 `env:"IEC101CTL_PORT" desc:"Terminal server TCP port (used when --url is not set; 0 = 2401)" flag:"port"`
	Timeout      uint32 `env:"IEC101CTL_TIMEOUT" desc:"Timeout in milliseconds for bringing up the link and for each request (0 = 10000)" flag:"timeout"`
	PollInterval uint32 `env:"IEC101CTL_POLL_INTERVAL" desc:"Unbalanced: milliseconds between class 2 polls when the station has nothing to send (0 = 100)" flag:"poll-interval"`
	Debug        bool   // set from global --debug (root persistent flag)
}

// StationConfig is a connection plus the common address of the station that is addressed.
type StationConfig struct {
	ConnConfig
	CommonAddr uint16 `env:"IEC101CTL_CA" desc:"Common address of the station (ASDU address)" flag:"ca"`
}

// InterrogateConfig drives `client interrogate`.
type InterrogateConfig struct {
	StationConfig
	Group        uint8  `env:"IEC101CTL_GROUP" desc:"Interrogation group 1-16, or 0 for a general (station) interrogation" flag:"group"`
	Types        string `env:"IEC101CTL_TYPES" desc:"Only show these types: comma-separated names or numbers (e.g. M_SP_NA_1,M_ME_NC_1 or 1,13)" flag:"type"`
	IOAs         string `env:"IEC101CTL_IOA" desc:"Only show these information object addresses: list and ranges (e.g. 100,200-299)" flag:"ioa"`
	OutputFormat string `env:"IEC101CTL_FORMAT" desc:"Output format: text, json, or table" flag:"format"`
}

// CountersConfig drives `client counters`.
type CountersConfig struct {
	StationConfig
	Group        uint8  `env:"IEC101CTL_GROUP" desc:"Counter group 1-4, or 0 for a general counter request" flag:"group"`
	Freeze       string `env:"IEC101CTL_FREEZE" desc:"What the request does to the counters: read, freeze, freeze-reset or reset" flag:"freeze"`
	OutputFormat string `env:"IEC101CTL_FORMAT" desc:"Output format: text, json, or table" flag:"format"`
}

// ReadConfig drives `client read`.
type ReadConfig struct {
	StationConfig
	IOAs         string `env:"IEC101CTL_IOA" desc:"Information object addresses to read: list and ranges (e.g. 100,200-203)" flag:"ioa"`
	OutputFormat string `env:"IEC101CTL_FORMAT" desc:"Output format: text, json, or table" flag:"format"`
}

// MonitorConfig drives `client monitor`.
type MonitorConfig struct {
	ConnConfig
	CommonAddr   string `env:"IEC101CTL_CA" desc:"Only show this common address; also the station that --interrogate addresses (default: all stations, interrogation by broadcast)" flag:"ca"`
	Interrogate  bool   `env:"IEC101CTL_INTERROGATE" desc:"Run a general interrogation each time the link comes up, so the stream starts with all current values" flag:"interrogate"`
	Types        string `env:"IEC101CTL_TYPES" desc:"Only show these types: comma-separated names or numbers" flag:"type"`
	IOAs         string `env:"IEC101CTL_IOA" desc:"Only show these information object addresses: list and ranges" flag:"ioa"`
	Duration     uint32 `env:"IEC101CTL_DURATION" desc:"Stop after this many seconds (0 = until interrupted)" flag:"duration"`
	Count        uint32 `env:"IEC101CTL_COUNT" desc:"Stop after this many values (0 = until interrupted)" flag:"count"`
	All          bool   `env:"IEC101CTL_ALL" desc:"Also show what is not process information: confirmations and terminations of commands" flag:"all"`
	NoReconnect  bool   `env:"IEC101CTL_NO_RECONNECT" desc:"Exit when the line is lost instead of opening it again" flag:"no-reconnect"`
	OutputFormat string `env:"IEC101CTL_FORMAT" desc:"Output format: text, json, or table" flag:"format"`
}

// CommandConfig drives `client command`.
type CommandConfig struct {
	StationConfig
	Type         string `env:"IEC101CTL_COMMAND_TYPE" desc:"Command type: single, double, step, setpoint-normalized, setpoint-scaled, setpoint-float or bitstring (or the type name, e.g. C_SC_NA_1)" flag:"type"`
	IOA          uint32 `env:"IEC101CTL_IOA" desc:"Information object address of the command" flag:"ioa"`
	Value        string `env:"IEC101CTL_VALUE" desc:"Value: on/off (single, double), higher/lower (step), a number (set points), a 32-bit number or 0x hex (bitstring)" flag:"value"`
	Mode         string `env:"IEC101CTL_COMMAND_MODE" desc:"Procedure: execute (direct), select (select only), sbo (select, then execute) or cancel (select, then deactivate the selection)" flag:"mode"`
	Qualifier    string `env:"IEC101CTL_QUALIFIER" desc:"Qualifier of command: none, short (short pulse), long (long pulse) or persistent; a number 0-127 for set points" flag:"qualifier"`
	Termination  uint32 `env:"IEC101CTL_TERMINATION" desc:"Also wait up to this many milliseconds for the activation termination after the execute (0 = do not wait)" flag:"wait-termination"`
	OutputFormat string `env:"IEC101CTL_FORMAT" desc:"Output format: text, json, or table" flag:"format"`
}

// ClockConfig drives `client clock`.
type ClockConfig struct {
	StationConfig
	Time         string `env:"IEC101CTL_TIME" desc:"Time to set: now, or an RFC 3339 time such as 2026-01-31T12:00:00Z" flag:"time"`
	OutputFormat string `env:"IEC101CTL_FORMAT" desc:"Output format: text, json, or table" flag:"format"`
}

// ResetConfig drives `client reset`.
type ResetConfig struct {
	StationConfig
	Qualifier    string `env:"IEC101CTL_RESET_QUALIFIER" desc:"What to reset: process (general reset of the process) or events (reset of pending information with time tag)" flag:"qualifier"`
	OutputFormat string `env:"IEC101CTL_FORMAT" desc:"Output format: text, json, or table" flag:"format"`
}

// DelayConfig drives `client delay`.
type DelayConfig struct {
	StationConfig
	Set          string `env:"IEC101CTL_SET_DELAY" desc:"Send the transmission delay to the station: auto (the delay just measured) or milliseconds 0-59999 (sent without measuring); default: measure only" flag:"set"`
	OutputFormat string `env:"IEC101CTL_FORMAT" desc:"Output format: text, json, or table" flag:"format"`
}

// PingConfig drives `client ping`.
type PingConfig struct {
	StationConfig
	Method       string `env:"IEC101CTL_PING_METHOD" desc:"What to send: command (test command C_TS_NA_1), link-status (request status of link) or link-test (test function for link, balanced only)" flag:"method"`
	Count        uint32 `env:"IEC101CTL_COUNT" desc:"Number of tests before exiting (0 = until interrupted)" flag:"count"`
	Interval     uint32 `env:"IEC101CTL_INTERVAL" desc:"Interval between tests in milliseconds" flag:"interval"`
	OutputFormat string `env:"IEC101CTL_FORMAT" desc:"Output format: text, json, or table" flag:"format"`
}

// ScanConfig drives `client scan`.
type ScanConfig struct {
	ConnConfig
	Method       string `env:"IEC101CTL_SCAN_METHOD" desc:"What to find: broadcast (one general interrogation to the broadcast common address), ca (one interrogation per common address) or link (request status of link per link address, unbalanced)" flag:"method"`
	Start        uint16 `env:"IEC101CTL_START" desc:"First address of a ca or link sweep" flag:"start"`
	End          uint16 `env:"IEC101CTL_END" desc:"Last address of a ca or link sweep" flag:"end"`
	Delay        uint32 `env:"IEC101CTL_DELAY" desc:"Delay in milliseconds between the requests of a sweep" flag:"delay"`
	OutputFormat string `env:"IEC101CTL_FORMAT" desc:"Output format: text, json, or table" flag:"format"`
}

// TypesConfig drives `client types`.
type TypesConfig struct {
	Direction    string `env:"IEC101CTL_DIRECTION" desc:"Only list types of this direction: monitor or control" flag:"direction"`
	OutputFormat string `env:"IEC101CTL_FORMAT" desc:"Output format: text, json, or table" flag:"format"`
}

// DiscoverConfig drives `discover`.
type DiscoverConfig struct {
	Subnets      []string `env:"IEC101CTL_SUBNETS" desc:"Subnets, ranges or addresses to probe: CIDR (192.168.1.0/24), range (192.168.1.10-20) or a single address; comma-separated" flag:"subnets"`
	Ports        []string `env:"IEC101CTL_PORTS" desc:"TCP ports to probe (terminal servers)" flag:"ports"`
	Mode         string   `env:"IEC101CTL_LINK_MODE" desc:"Link transmission of the stations: unbalanced or balanced" flag:"link-mode"`
	LinkAddr     uint16   `env:"IEC101CTL_LINK_ADDRESS" desc:"Link address to ask for the status of link" flag:"link-address"`
	LinkAddrSize uint8    `env:"IEC101CTL_LINK_ADDRESS_SIZE" desc:"Octets of the link address field: 1 or 2, or 0 in balanced mode" flag:"link-address-size"`
	Timeout      uint32   `env:"IEC101CTL_TIMEOUT" desc:"Timeout in milliseconds per probe (0 = 1000)" flag:"timeout"`
	Parallel     uint16   `env:"IEC101CTL_PARALLEL" desc:"Number of concurrent probes (1-256)" flag:"parallel"`
	ForceLarge   bool     `env:"IEC101CTL_DISCOVER_FORCE_LARGE" desc:"Allow a scan of more hosts than the built-in safety cap" flag:"force-large-scan"`
	OutputFormat string   `env:"IEC101CTL_FORMAT" desc:"Output format: text, json, or table" flag:"format"`
	Debug        bool     // set from global --debug (root persistent flag)
}

// ServerConfig drives `server start`.
type ServerConfig struct {
	LineConfig
	LinkConfig
	Config        string `env:"IEC101CTL_CONFIG" desc:"Station model file (YAML or JSON); without it a built-in demo station is served" flag:"config"`
	Listen        string `env:"IEC101CTL_LISTEN" desc:"Address to listen on for a line over TCP" flag:"listen"`
	Port          uint16 `env:"IEC101CTL_PORT" desc:"TCP port to listen on (0 = 2401)" flag:"port"`
	Serial        string `env:"IEC101CTL_SERIAL" desc:"Serve on this serial port (e.g. /dev/ttyUSB0 or COM3) instead of TCP" flag:"serial"`
	SingleCharAck bool   `env:"IEC101CTL_SINGLE_CHAR_ACK" desc:"Unbalanced: acknowledge with the single character E5h instead of a fixed-length frame" flag:"single-char-ack"`
	NoEndOfInit   bool   `env:"IEC101CTL_NO_END_OF_INIT" desc:"Do not send end of initialization (M_EI_NA_1) after the first reset of the link" flag:"no-end-of-init"`
	Duration      uint32 `env:"IEC101CTL_DURATION" desc:"Stop after this many seconds (0 = until interrupted)" flag:"duration"`
	OutputFormat  string `env:"IEC101CTL_FORMAT" desc:"Format of the event log on stdout: text, json, or table" flag:"format"`
	Debug         bool   // set from global --debug (root persistent flag)
}

// GenerateConfigConfig drives `server generate-config`.
type GenerateConfigConfig struct {
	Output string `env:"IEC101CTL_OUTPUT" desc:"File to write the model to (- = stdout)" flag:"output"`
	Format string `env:"IEC101CTL_CONFIG_FORMAT" desc:"Format of the model: yaml or json" flag:"format"`
	Force  bool   `env:"IEC101CTL_FORCE" desc:"Overwrite an existing file" flag:"force"`
}
