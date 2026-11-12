// SPDX-License-Identifier: MIT

package config

// TLSConfig holds the TLS settings of a connection (IEC 62351-3). On a client they apply to
// tls:// URLs, or to --ip when --tls is set; on a server they turn the listener into a TLS one.
type TLSConfig struct {
	TLS        bool   `env:"IEC104CTL_TLS" desc:"Use TLS with --ip (implied by a tls:// URL)" flag:"tls"`
	CACert     string `env:"IEC104CTL_TLS_CA" desc:"PEM file with the CA certificates to verify the peer with (default: system roots)" flag:"tls-ca"`
	Cert       string `env:"IEC104CTL_TLS_CERT" desc:"PEM file with the own certificate (client authentication)" flag:"tls-cert"`
	Key        string `env:"IEC104CTL_TLS_KEY" desc:"PEM file with the private key of --tls-cert" flag:"tls-key"`
	ServerName string `env:"IEC104CTL_TLS_SERVER_NAME" desc:"Name to verify the station certificate against (default: the host)" flag:"tls-server-name"`
	Insecure   bool   `env:"IEC104CTL_TLS_INSECURE" desc:"Do not verify the station certificate (testing only)" flag:"tls-insecure"`
}

// SecurityConfig holds the application-layer security of IEC 62351-5, the A-Profile of
// IEC TS 60870-5-7, that client and server share: the station's certificate and key, and
// the remote stations it trusts. Without --secure every other security flag is invalid.
type SecurityConfig struct {
	Secure        bool   `env:"IEC104CTL_SECURE" desc:"Run the application-layer security of IEC 62351-5 (A-Profile of IEC TS 60870-5-7): a Station Association with certificates, then every ASDU as Secure Data" flag:"secure"`
	SecurityCert  string `env:"IEC104CTL_SECURITY_CERT" desc:"PEM file with the own station certificate (P-256, or X25519 signed by an authority); needs --secure" flag:"security-cert"`
	SecurityKey   string `env:"IEC104CTL_SECURITY_KEY" desc:"PEM file with the private key of --security-cert (PKCS #8 or SEC 1); needs --secure" flag:"security-key"`
	SecurityTrust string `env:"IEC104CTL_SECURITY_TRUST" desc:"PEM file with the certificates of the remote stations to trust: a CA certificate is an authority (the station certificates it signed are accepted), any other is a pinned self-signed station certificate; needs --secure" flag:"security-trust"`
}

// ClientSecurityConfig is the security of a controlling station (every client command).
type ClientSecurityConfig struct {
	SecurityConfig
	AssociationID uint16 `env:"IEC104CTL_ASSOCIATION_ID" desc:"AIM: the association ID of this controlling station, 1-65535; needs --secure" flag:"association-id"`
	MAL           string `env:"IEC104CTL_MAL" desc:"MAC algorithm the controlling station selects, by code or name: 4 or hmac-sha-256 (default), 3 or hmac-sha-256/8, 8 or hmac-sha3-256, 7 or hmac-sha3-256/8; needs --secure" flag:"mal"`
	DPA           string `env:"IEC104CTL_DPA" desc:"Data protection algorithm of Secure Data, by code or name: a MAC algorithm as for --mal, or 11 or aes-256-gcm to encrypt as well (default: --mal); needs --secure" flag:"dpa"`
}

// ServerSecurityConfig is the security of the simulated controlled station.
type ServerSecurityConfig struct {
	SecurityConfig
	AssociationID uint16   `env:"IEC104CTL_ASSOCIATION_ID" desc:"AIS: the association ID of this controlled station, 1-65535; needs --secure" flag:"association-id"`
	Roles         []string `env:"IEC104CTL_ROLES" desc:"Roles every controlling station has: viewer, operator, engineer, installer, secadm, secaud, rbacmnt (default: no access control, everything is permitted); needs --secure" flag:"roles"`
}

// LinkConfig holds the protocol parameters of IEC 60870-5-104 clause 9 and the field sizes
// of the ASDU. Zero selects the standard value. Both ends of a connection must agree.
type LinkConfig struct {
	K  uint16 `env:"IEC104CTL_K" desc:"k: maximum number of unacknowledged I frames sent (0 = 12)" flag:"k"`
	W  uint16 `env:"IEC104CTL_W" desc:"w: acknowledge after at most this many received I frames (0 = 8)" flag:"w"`
	T0 uint16 `env:"IEC104CTL_T0" desc:"t0: connection establishment timeout in seconds (0 = 30)" flag:"t0"`
	T1 uint16 `env:"IEC104CTL_T1" desc:"t1: timeout in seconds for the acknowledgement of a sent frame (0 = 15)" flag:"t1"`
	T2 uint16 `env:"IEC104CTL_T2" desc:"t2: acknowledge received I frames after this many seconds without sending (0 = 10)" flag:"t2"`
	T3 uint16 `env:"IEC104CTL_T3" desc:"t3: send a test frame after this many seconds of inactivity (0 = 20)" flag:"t3"`

	CauseSize uint8  `env:"IEC104CTL_COT_SIZE" desc:"Octets of the cause of transmission, 1 or 2 (0 = 2, the IEC 104 value)" flag:"cot-size"`
	CASize    uint8  `env:"IEC104CTL_CA_SIZE" desc:"Octets of the common address, 1 or 2 (0 = 2, the IEC 104 value)" flag:"ca-size"`
	IOASize   uint8  `env:"IEC104CTL_IOA_SIZE" desc:"Octets of the information object address, 1 to 3 (0 = 3, the IEC 104 value)" flag:"ioa-size"`
	TimeZone  string `env:"IEC104CTL_TIMEZONE" desc:"Time zone of the time tags on the wire: utc, local or an IANA name such as Europe/Brussels (default utc)" flag:"timezone"`
}

// ConnConfig is what every client command needs to reach a station.
type ConnConfig struct {
	TLSConfig
	ClientSecurityConfig
	LinkConfig
	URL        string `env:"IEC104CTL_URL" desc:"Station URL: tcp://host[:port] or tls://host[:port] (ports default to 2404 and 19998); mutually exclusive with --ip/--port" flag:"url"`
	IP         string `env:"IEC104CTL_IP" desc:"Station IP address or host name (used when --url is not set)" flag:"ip"`
	Port       uint16 `env:"IEC104CTL_PORT" desc:"Station TCP port (used when --url is not set)" flag:"port"`
	Originator uint8  `env:"IEC104CTL_ORIGINATOR" desc:"Originator address sent with every request (0 = not used)" flag:"originator"`
	Timeout    uint32 `env:"IEC104CTL_TIMEOUT" desc:"Timeout in milliseconds for connecting and for each request (0 = 10000)" flag:"timeout"`
	Debug      bool   // set from global --debug (root persistent flag)
	// CommonAddrHint is the common address of the station a command addresses, when it
	// addresses one; it fills the data unit identifier of the security messages.
	CommonAddrHint uint16
}

func (c *ConnConfig) wantsTLS() bool {
	t := c.TLSConfig
	return t.TLS || t.CACert != "" || t.Cert != "" || t.Key != "" || t.ServerName != "" || t.Insecure
}

// StationConfig is a connection plus the common address of the station that is addressed.
type StationConfig struct {
	ConnConfig
	CommonAddr uint16 `env:"IEC104CTL_CA" desc:"Common address of the station (ASDU address)" flag:"ca"`
}

// Conn returns the connection of c, with the common address of the station as the hint
// for the security messages.
func (c *StationConfig) Conn() *ConnConfig {
	c.CommonAddrHint = c.CommonAddr
	return &c.ConnConfig
}

// InterrogateConfig drives `client interrogate`.
type InterrogateConfig struct {
	StationConfig
	Group        uint8  `env:"IEC104CTL_GROUP" desc:"Interrogation group 1-16, or 0 for a general (station) interrogation" flag:"group"`
	Types        string `env:"IEC104CTL_TYPES" desc:"Only show these types: comma-separated names or numbers (e.g. M_SP_NA_1,M_ME_NC_1 or 1,13)" flag:"type"`
	IOAs         string `env:"IEC104CTL_IOA" desc:"Only show these information object addresses: list and ranges (e.g. 100,200-299)" flag:"ioa"`
	OutputFormat string `env:"IEC104CTL_FORMAT" desc:"Output format: text, json, or table" flag:"format"`
}

// CountersConfig drives `client counters`.
type CountersConfig struct {
	StationConfig
	Group        uint8  `env:"IEC104CTL_GROUP" desc:"Counter group 1-4, or 0 for a general counter request" flag:"group"`
	Freeze       string `env:"IEC104CTL_FREEZE" desc:"What the request does to the counters: read, freeze, freeze-reset or reset" flag:"freeze"`
	OutputFormat string `env:"IEC104CTL_FORMAT" desc:"Output format: text, json, or table" flag:"format"`
}

// ReadConfig drives `client read`.
type ReadConfig struct {
	StationConfig
	IOAs         string `env:"IEC104CTL_IOA" desc:"Information object addresses to read: list and ranges (e.g. 100,200-203)" flag:"ioa"`
	OutputFormat string `env:"IEC104CTL_FORMAT" desc:"Output format: text, json, or table" flag:"format"`
}

// MonitorConfig drives `client monitor`.
type MonitorConfig struct {
	ConnConfig
	CommonAddr   string `env:"IEC104CTL_CA" desc:"Only show this common address; also the station that --interrogate addresses (default: all stations, interrogation by broadcast)" flag:"ca"`
	Interrogate  bool   `env:"IEC104CTL_INTERROGATE" desc:"Run a general interrogation after connecting (and after every reconnect), so the stream starts with all current values" flag:"interrogate"`
	Types        string `env:"IEC104CTL_TYPES" desc:"Only show these types: comma-separated names or numbers" flag:"type"`
	IOAs         string `env:"IEC104CTL_IOA" desc:"Only show these information object addresses: list and ranges" flag:"ioa"`
	Duration     uint32 `env:"IEC104CTL_DURATION" desc:"Stop after this many seconds (0 = until interrupted)" flag:"duration"`
	Count        uint32 `env:"IEC104CTL_COUNT" desc:"Stop after this many values (0 = until interrupted)" flag:"count"`
	All          bool   `env:"IEC104CTL_ALL" desc:"Also show what is not process information: confirmations and terminations of commands, file transfer" flag:"all"`
	NoReconnect  bool   `env:"IEC104CTL_NO_RECONNECT" desc:"Exit when the connection is lost instead of reconnecting" flag:"no-reconnect"`
	OutputFormat string `env:"IEC104CTL_FORMAT" desc:"Output format: text, json, or table" flag:"format"`
}

// CommandConfig drives `client command`.
type CommandConfig struct {
	StationConfig
	Type         string `env:"IEC104CTL_COMMAND_TYPE" desc:"Command type: single, double, step, setpoint-normalized, setpoint-scaled, setpoint-float or bitstring (or the type name, e.g. C_SC_NA_1)" flag:"type"`
	IOA          uint32 `env:"IEC104CTL_IOA" desc:"Information object address of the command" flag:"ioa"`
	Value        string `env:"IEC104CTL_VALUE" desc:"Value: on/off (single, double), higher/lower (step), a number (set points), a 32-bit number or 0x hex (bitstring)" flag:"value"`
	Mode         string `env:"IEC104CTL_COMMAND_MODE" desc:"Procedure: execute (direct), select (select only), sbo (select, then execute) or cancel (select, then deactivate the selection)" flag:"mode"`
	Qualifier    string `env:"IEC104CTL_QUALIFIER" desc:"Qualifier of command: none, short (short pulse), long (long pulse) or persistent; a number 0-127 for set points" flag:"qualifier"`
	TimeTag      bool   `env:"IEC104CTL_TIME_TAG" desc:"Send the command with a time tag (the CP56Time2a variant, e.g. C_SC_TA_1)" flag:"time-tag"`
	Termination  uint32 `env:"IEC104CTL_TERMINATION" desc:"Also wait up to this many milliseconds for the activation termination after the execute (0 = do not wait)" flag:"wait-termination"`
	OutputFormat string `env:"IEC104CTL_FORMAT" desc:"Output format: text, json, or table" flag:"format"`
}

// ClockConfig drives `client clock`.
type ClockConfig struct {
	StationConfig
	Time         string `env:"IEC104CTL_TIME" desc:"Time to set: now, or an RFC 3339 time such as 2026-01-31T12:00:00Z" flag:"time"`
	OutputFormat string `env:"IEC104CTL_FORMAT" desc:"Output format: text, json, or table" flag:"format"`
}

// ResetConfig drives `client reset`.
type ResetConfig struct {
	StationConfig
	Qualifier    string `env:"IEC104CTL_RESET_QUALIFIER" desc:"What to reset: process (general reset of the process) or events (reset of pending information with time tag)" flag:"qualifier"`
	OutputFormat string `env:"IEC104CTL_FORMAT" desc:"Output format: text, json, or table" flag:"format"`
}

// PingConfig drives `client ping`.
type PingConfig struct {
	StationConfig
	Method       string `env:"IEC104CTL_PING_METHOD" desc:"What to send: testfr (link test frame, TESTFR act/con) or command (test command C_TS_TA_1, through the station's application)" flag:"method"`
	Count        uint32 `env:"IEC104CTL_COUNT" desc:"Number of tests before exiting (0 = until interrupted)" flag:"count"`
	Interval     uint32 `env:"IEC104CTL_INTERVAL" desc:"Interval between tests in milliseconds" flag:"interval"`
	OutputFormat string `env:"IEC104CTL_FORMAT" desc:"Output format: text, json, or table" flag:"format"`
}

// ScanConfig drives `client scan`.
type ScanConfig struct {
	ConnConfig
	Method       string `env:"IEC104CTL_SCAN_METHOD" desc:"How to find stations: broadcast (one general interrogation to the broadcast address) or sweep (one interrogation per common address)" flag:"method"`
	Start        uint16 `env:"IEC104CTL_START" desc:"First common address of a sweep" flag:"start"`
	End          uint16 `env:"IEC104CTL_END" desc:"Last common address of a sweep" flag:"end"`
	Delay        uint32 `env:"IEC104CTL_DELAY" desc:"Delay in milliseconds between the requests of a sweep" flag:"delay"`
	OutputFormat string `env:"IEC104CTL_FORMAT" desc:"Output format: text, json, or table" flag:"format"`
}

// FileListConfig drives `client file list`.
type FileListConfig struct {
	StationConfig
	IOA          uint32 `env:"IEC104CTL_IOA" desc:"Information object address of the directory (0 = the station's default directory)" flag:"ioa"`
	OutputFormat string `env:"IEC104CTL_FORMAT" desc:"Output format: text, json, or table" flag:"format"`
}

// FileGetConfig drives `client file get`.
type FileGetConfig struct {
	StationConfig
	IOA          uint32 `env:"IEC104CTL_IOA" desc:"Information object address of the file" flag:"ioa"`
	Name         uint16 `env:"IEC104CTL_FILE_NAME" desc:"Name of file (NOF), a number 0-65535" flag:"name"`
	Output       string `env:"IEC104CTL_OUTPUT" desc:"File to write the content to (- = stdout)" flag:"output"`
	OutputFormat string `env:"IEC104CTL_FORMAT" desc:"Output format: text, json, or table" flag:"format"`
}

// FilePutConfig drives `client file put`.
type FilePutConfig struct {
	StationConfig
	IOA          uint32 `env:"IEC104CTL_IOA" desc:"Information object address of the file" flag:"ioa"`
	Name         uint16 `env:"IEC104CTL_FILE_NAME" desc:"Name of file (NOF), a number 0-65535" flag:"name"`
	Input        string `env:"IEC104CTL_INPUT" desc:"File whose content is sent" flag:"input"`
	SectionSize  uint32 `env:"IEC104CTL_SECTION_SIZE" desc:"Octets per section (0 = the whole file as one section)" flag:"section-size"`
	OutputFormat string `env:"IEC104CTL_FORMAT" desc:"Output format: text, json, or table" flag:"format"`
}

// TypesConfig drives `client types`.
type TypesConfig struct {
	Direction    string `env:"IEC104CTL_DIRECTION" desc:"Only list types of this direction: monitor or control" flag:"direction"`
	OutputFormat string `env:"IEC104CTL_FORMAT" desc:"Output format: text, json, or table" flag:"format"`
}

// DiscoverConfig drives `discover`.
type DiscoverConfig struct {
	Subnets      []string `env:"IEC104CTL_SUBNETS" desc:"Subnets, ranges or addresses to probe: CIDR (192.168.1.0/24), range (192.168.1.10-20) or a single address; comma-separated" flag:"subnets"`
	Ports        []string `env:"IEC104CTL_PORTS" desc:"TCP ports to probe" flag:"ports"`
	Timeout      uint32   `env:"IEC104CTL_TIMEOUT" desc:"Timeout in milliseconds per probe (0 = 1000)" flag:"timeout"`
	Parallel     uint16   `env:"IEC104CTL_PARALLEL" desc:"Number of concurrent probes (1-256)" flag:"parallel"`
	ForceLarge   bool     `env:"IEC104CTL_DISCOVER_FORCE_LARGE" desc:"Allow a scan of more hosts than the built-in safety cap" flag:"force-large-scan"`
	OutputFormat string   `env:"IEC104CTL_FORMAT" desc:"Output format: text, json, or table" flag:"format"`
	Debug        bool     // set from global --debug (root persistent flag)
}

// ServerConfig drives `server start`.
type ServerConfig struct {
	LinkConfig
	ServerSecurityConfig
	Config       string   `env:"IEC104CTL_CONFIG" desc:"Station model file (YAML or JSON); without it a built-in demo station is served" flag:"config"`
	Listen       string   `env:"IEC104CTL_LISTEN" desc:"Address to listen on" flag:"listen"`
	Port         uint16   `env:"IEC104CTL_PORT" desc:"TCP port to listen on (0 = 2404, or 19998 with TLS)" flag:"port"`
	Cert         string   `env:"IEC104CTL_TLS_CERT" desc:"PEM file with the server certificate; with --tls-key it makes the server a TLS server" flag:"tls-cert"`
	Key          string   `env:"IEC104CTL_TLS_KEY" desc:"PEM file with the private key of --tls-cert" flag:"tls-key"`
	ClientCA     string   `env:"IEC104CTL_TLS_CLIENT_CA" desc:"PEM file with the CA certificates clients must present a certificate of (mutual TLS)" flag:"tls-client-ca"`
	Allow        []string `env:"IEC104CTL_ALLOW" desc:"Only accept clients from these addresses or CIDR prefixes (default: any)" flag:"allow"`
	MaxSessions  uint16   `env:"IEC104CTL_MAX_SESSIONS" desc:"Maximum number of simultaneous connections (0 = no limit)" flag:"max-sessions"`
	Duration     uint32   `env:"IEC104CTL_DURATION" desc:"Stop after this many seconds (0 = until interrupted)" flag:"duration"`
	OutputFormat string   `env:"IEC104CTL_FORMAT" desc:"Format of the event log on stdout: text, json, or table" flag:"format"`
	Debug        bool     // set from global --debug (root persistent flag)
}

// GenerateConfigConfig drives `server generate-config`.
type GenerateConfigConfig struct {
	Output string `env:"IEC104CTL_OUTPUT" desc:"File to write the model to (- = stdout)" flag:"output"`
	Format string `env:"IEC104CTL_CONFIG_FORMAT" desc:"Format of the model: yaml or json" flag:"format"`
	Force  bool   `env:"IEC104CTL_FORCE" desc:"Overwrite an existing file" flag:"force"`
}
