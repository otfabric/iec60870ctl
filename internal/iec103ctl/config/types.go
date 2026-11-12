// SPDX-License-Identifier: MIT

// Package config holds the configuration of every iec103ctl command: struct fields bound
// to a flag (tag flag), an environment variable (tag env, prefix IEC103CTL_) and a help
// text (tag desc).
package config

// SerialConfig is the character format of a serial line (IEC 60870-5-1 FT 1.2). Zero
// values select the FT 1.2 format: 9600 baud, 8 data bits, even parity, 1 stop bit. On a
// TCP line the terminal server sets the line and these flags do not apply.
type SerialConfig struct {
	Baud     uint32 `env:"IEC103CTL_BAUD" desc:"Serial line: bit rate (0 = 9600; IEC 60870-5-103 names 9600 and 19200)" flag:"baud"`
	DataBits uint8  `env:"IEC103CTL_DATA_BITS" desc:"Serial line: data bits (0 = 8)" flag:"data-bits"`
	Parity   string `env:"IEC103CTL_PARITY" desc:"Serial line: parity, even (FT 1.2), odd or none" flag:"parity"`
	StopBits uint8  `env:"IEC103CTL_STOP_BITS" desc:"Serial line: stop bits, 1 or 2 (0 = 1)" flag:"stop-bits"`
}

// LinkConfig is the link layer of the controlling system (IEC 60870-5-2, unbalanced, as
// IEC 60870-5-103 clause 6.2 profiles it).
type LinkConfig struct {
	LinkAddress   uint16 `env:"IEC103CTL_LINK_ADDRESS" desc:"Link address of the protection equipment, 0-254 (255 is the broadcast address)" flag:"link-address"`
	CommonAddress uint16 `env:"IEC103CTL_COMMON_ADDRESS" desc:"Common address of ASDU, 1-255 (0 = the link address, the normal case of clause 7.2.4)" flag:"common-address"`
	LinkTimeout   uint32 `env:"IEC103CTL_LINK_TIMEOUT" desc:"Milliseconds to wait for the answer to a frame before it is repeated (0 = 1000)" flag:"link-timeout"`
	Retries       int    `env:"IEC103CTL_RETRIES" desc:"How often an unanswered frame is repeated, 0-10" flag:"retries"`
	ResetFCB      bool   `env:"IEC103CTL_RESET_FCB" desc:"Initialize the link with reset FCB (function 7) instead of reset CU (function 0)" flag:"reset-fcb"`
	PollInterval  uint32 `env:"IEC103CTL_POLL_INTERVAL" desc:"Milliseconds between class 2 polls when the equipment has nothing to send (0 = 100)" flag:"poll-interval"`
	TimeZone      string `env:"IEC103CTL_TIMEZONE" desc:"Time zone of the time tags on the wire: utc, local or an IANA name such as Europe/Brussels (default utc)" flag:"timezone"`
}

// ConnConfig is what every client command needs to reach the protection equipment.
type ConnConfig struct {
	SerialConfig
	LinkConfig
	URL     string `env:"IEC103CTL_URL" desc:"Line: tcp://host[:port] (a terminal server, port 2401 by default) or serial:///dev/ttyUSB0; mutually exclusive with --ip/--port" flag:"url"`
	IP      string `env:"IEC103CTL_IP" desc:"Host of a terminal server (used when --url is not set)" flag:"ip"`
	Port    uint16 `env:"IEC103CTL_PORT" desc:"TCP port of the terminal server (0 = 2401; used with --ip)" flag:"port"`
	Timeout uint32 `env:"IEC103CTL_TIMEOUT" desc:"Milliseconds for connecting and the link start-up, and for each request (0 = 10000)" flag:"timeout"`
	Debug   bool   // set from global --debug (root persistent flag)
}

// InterrogateConfig drives `client interrogate`.
type InterrogateConfig struct {
	ConnConfig
	OutputFormat string `env:"IEC103CTL_FORMAT" desc:"Output format: text, json, or table" flag:"format"`
}

// CommandConfig drives `client command`.
type CommandConfig struct {
	ConnConfig
	Fun          uint16 `env:"IEC103CTL_FUN" desc:"Function type (FUN) of the command, 0-255" flag:"fun"`
	Inf          uint16 `env:"IEC103CTL_INF" desc:"Information number (INF) of the command, 0-255" flag:"inf"`
	Value        string `env:"IEC103CTL_VALUE" desc:"Double command (DCO): on (2) or off (1)" flag:"value"`
	WaitReport   uint32 `env:"IEC103CTL_WAIT_REPORT" desc:"After the acknowledgement, wait up to this many milliseconds for the change of state (cause 12) (0 = do not wait)" flag:"wait-report"`
	OutputFormat string `env:"IEC103CTL_FORMAT" desc:"Output format: text, json, or table" flag:"format"`
}

// ClockConfig drives `client clock`.
type ClockConfig struct {
	ConnConfig
	Time         string `env:"IEC103CTL_TIME" desc:"Time to set: now, or an RFC 3339 time such as 2026-01-31T12:00:00Z" flag:"time"`
	Broadcast    bool   `env:"IEC103CTL_BROADCAST" desc:"Send the synchronization to every device on the line (link address 255, send/no reply): nothing confirms it" flag:"broadcast"`
	OutputFormat string `env:"IEC103CTL_FORMAT" desc:"Output format: text, json, or table" flag:"format"`
}

// IdentifyConfig drives `client identify`.
type IdentifyConfig struct {
	ConnConfig
	Wait         uint32 `env:"IEC103CTL_WAIT" desc:"After the identification of the reset arrived, wait this many milliseconds for others (start/restart, power on)" flag:"wait"`
	OutputFormat string `env:"IEC103CTL_FORMAT" desc:"Output format: text, json, or table" flag:"format"`
}

// GenericConfig drives `client generic headings` and `client generic interrogate`.
type GenericConfig struct {
	ConnConfig
	OutputFormat string `env:"IEC103CTL_FORMAT" desc:"Output format: text, json, or table" flag:"format"`
}

// GenericReadConfig drives `client generic read`.
type GenericReadConfig struct {
	ConnConfig
	Inf          uint16 `env:"IEC103CTL_INF" desc:"Information number of the read: 240 (headings), 241 (group), 243 (directory) or 244 (entry)" flag:"inf"`
	Items        string `env:"IEC103CTL_ITEMS" desc:"Generic identifiers to read: GROUP.ENTRY[:KOD], comma-separated (KOD defaults to 1, the actual value), e.g. 1.2,1.3:10" flag:"items"`
	OutputFormat string `env:"IEC103CTL_FORMAT" desc:"Output format: text, json, or table" flag:"format"`
}

// GenericEntryConfig drives `client generic entry` and `client generic directory`.
type GenericEntryConfig struct {
	ConnConfig
	GIN          string `env:"IEC103CTL_GIN" desc:"Generic identification number: GROUP.ENTRY, e.g. 1.2" flag:"gin"`
	KOD          string `env:"IEC103CTL_KOD" desc:"Kind of description: a number 0-255 or a name (actual, default, range, precision, factor, reference, enumeration, dimension, description, ...)" flag:"kod"`
	OutputFormat string `env:"IEC103CTL_FORMAT" desc:"Output format: text, json, or table" flag:"format"`
}

// GenericGroupConfig drives `client generic group`.
type GenericGroupConfig struct {
	ConnConfig
	Group        uint16 `env:"IEC103CTL_GROUP" desc:"Generic group, 0-255" flag:"group"`
	KOD          string `env:"IEC103CTL_KOD" desc:"Kind of description: a number 0-255 or a name (actual, description, ...)" flag:"kod"`
	OutputFormat string `env:"IEC103CTL_FORMAT" desc:"Output format: text, json, or table" flag:"format"`
}

// GenericWriteConfig drives `client generic write`.
type GenericWriteConfig struct {
	ConnConfig
	GIN          string `env:"IEC103CTL_GIN" desc:"Generic identification number of the entry: GROUP.ENTRY" flag:"gin"`
	KOD          string `env:"IEC103CTL_KOD" desc:"Kind of description to write (default actual, KOD 1)" flag:"kod"`
	Type         string `env:"IEC103CTL_DATA_TYPE" desc:"Data type of the value: ascii (1), uint (3), int (4), float32 (7) or float64 (8)" flag:"type"`
	Size         uint16 `env:"IEC103CTL_SIZE" desc:"DATASIZE: octets of the value (uint, int: 1-8; ascii: the length of the text when 0)" flag:"size"`
	Value        string `env:"IEC103CTL_VALUE" desc:"The value: text for ascii, a number for the others" flag:"value"`
	Mode         string `env:"IEC103CTL_WRITE_MODE" desc:"Procedure: execute (write with execution, INF 250) or abort (write entry, then write abort: nothing is activated)" flag:"mode"`
	OutputFormat string `env:"IEC103CTL_FORMAT" desc:"Output format: text, json, or table" flag:"format"`
}

// DisturbanceListConfig drives `client disturbance list`.
type DisturbanceListConfig struct {
	ConnConfig
	Fun          uint16 `env:"IEC103CTL_FUN" desc:"Function type of the order, 1-255 (0 = the function type of the equipment's identification)" flag:"fun"`
	OutputFormat string `env:"IEC103CTL_FORMAT" desc:"Output format: text, json, or table" flag:"format"`
}

// DisturbanceGetConfig drives `client disturbance get`.
type DisturbanceGetConfig struct {
	ConnConfig
	Fun             uint16 `env:"IEC103CTL_FUN" desc:"Function type of the orders, 1-255 (0 = the function type of the equipment's identification)" flag:"fun"`
	FAN             uint32 `env:"IEC103CTL_FAN" desc:"Fault number (FAN) of the recorded disturbance, 0-65535" flag:"fan"`
	TransferTimeout uint32 `env:"IEC103CTL_TRANSFER_TIMEOUT" desc:"Milliseconds the whole transfer may take (0 = 120000)" flag:"transfer-timeout"`
	Output          string `env:"IEC103CTL_OUTPUT" desc:"Also write the record (description, tags, channels with every value) as JSON to this file" flag:"output"`
	CSV             string `env:"IEC103CTL_CSV" desc:"Also write the channel values as CSV to this file: one row per value number, one column per channel (the raw SDV)" flag:"csv"`
	Force           bool   `env:"IEC103CTL_FORCE" desc:"Overwrite existing --output and --csv files" flag:"force"`
	OutputFormat    string `env:"IEC103CTL_FORMAT" desc:"Output format: text, json, or table" flag:"format"`
}

// MonitorConfig drives `client monitor`.
type MonitorConfig struct {
	ConnConfig
	Interrogate  bool   `env:"IEC103CTL_INTERROGATE" desc:"Run a general interrogation after the link start-up (and after every reconnect), so the stream starts with the current states" flag:"interrogate"`
	Duration     uint32 `env:"IEC103CTL_DURATION" desc:"Stop after this many seconds (0 = until interrupted)" flag:"duration"`
	Count        uint32 `env:"IEC103CTL_COUNT" desc:"Stop after this many messages (0 = until interrupted)" flag:"count"`
	NoReconnect  bool   `env:"IEC103CTL_NO_RECONNECT" desc:"Exit when the line is lost instead of connecting again" flag:"no-reconnect"`
	OutputFormat string `env:"IEC103CTL_FORMAT" desc:"Output format: text, json, or table" flag:"format"`
}

// ScanConfig drives `client scan`.
type ScanConfig struct {
	ConnConfig
	Start        uint16 `env:"IEC103CTL_START" desc:"First link address to try, 0-254" flag:"start"`
	End          uint16 `env:"IEC103CTL_END" desc:"Last link address to try, 0-254" flag:"end"`
	OutputFormat string `env:"IEC103CTL_FORMAT" desc:"Output format: text, json, or table" flag:"format"`
}

// ServerConfig drives `server start`.
type ServerConfig struct {
	SerialConfig
	Config        string `env:"IEC103CTL_CONFIG" desc:"Device model file (YAML or JSON); without it a built-in demo device is served" flag:"config"`
	Listen        string `env:"IEC103CTL_LISTEN" desc:"Address to listen on for the line over TCP" flag:"listen"`
	Port          uint16 `env:"IEC103CTL_PORT" desc:"TCP port to listen on (0 = 2401)" flag:"port"`
	Serial        string `env:"IEC103CTL_SERIAL" desc:"Serve a serial port instead of TCP, e.g. /dev/ttyUSB0 or COM3" flag:"serial"`
	LinkAddress   string `env:"IEC103CTL_LINK_ADDRESS" desc:"Link address of the device, 0-254 (default: the model's)" flag:"link-address"`
	CommonAddress string `env:"IEC103CTL_COMMON_ADDRESS" desc:"Common address of ASDU, 0-254 (default: the model's)" flag:"common-address"`
	TimeZone      string `env:"IEC103CTL_TIMEZONE" desc:"Time zone of the time tags on the wire: utc, local or an IANA name (default utc)" flag:"timezone"`
	Duration      uint32 `env:"IEC103CTL_DURATION" desc:"Stop after this many seconds (0 = until interrupted)" flag:"duration"`
	OutputFormat  string `env:"IEC103CTL_FORMAT" desc:"Format of the event log on stdout: text, json, or table" flag:"format"`
	Debug         bool   // set from global --debug (root persistent flag)
}

// GenerateConfigConfig drives `server generate-config`.
type GenerateConfigConfig struct {
	Output string `env:"IEC103CTL_OUTPUT" desc:"File to write the model to (- = stdout)" flag:"output"`
	Format string `env:"IEC103CTL_CONFIG_FORMAT" desc:"Format of the model: yaml or json" flag:"format"`
	Force  bool   `env:"IEC103CTL_FORCE" desc:"Overwrite an existing file" flag:"force"`
}
