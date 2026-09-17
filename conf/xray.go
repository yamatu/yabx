package conf

import (
	"os"
	"path/filepath"

	"github.com/goccy/go-json"
)

type XrayConfig struct {
	LogConfig          *XrayLogConfig        `json:"Log"`
	AssetPath          string                `json:"AssetPath"`
	DnsConfigPath      string                `json:"DnsConfigPath"`
	RouteConfigPath    string                `json:"RouteConfigPath"`
	ConnectionConfig   *XrayConnectionConfig `json:"XrayConnectionConfig"`
	InboundConfigPath  string                `json:"InboundConfigPath"`
	OutboundConfigPath string                `json:"OutboundConfigPath"`

	// outboundConfigPathSet records whether OutboundConfigPath was present in the
	// config file.
	//
	// The examples and the install wizard used to write RouteConfigPath without
	// OutboundConfigPath, which left the outbound tags referenced by the shipped
	// route.json (block, IPv4_out, IPv6_out) undefined. Xray then drops every
	// connection that matches such a rule and writes a warning per connection.
	// An absent key therefore falls back to the custom_outbound.json that
	// installers place next to AssetPath. Setting the key explicitly, even to an
	// empty string, always wins and disables the fallback.
	outboundConfigPathSet bool
}

type XrayLogConfig struct {
	Level      string `json:"Level"`
	AccessPath string `json:"AccessPath"`
	ErrorPath  string `json:"ErrorPath"`
}

type XrayConnectionConfig struct {
	Handshake    uint32 `json:"handshake"`
	ConnIdle     uint32 `json:"connIdle"`
	UplinkOnly   uint32 `json:"uplinkOnly"`
	DownlinkOnly uint32 `json:"downlinkOnly"`
	BufferSize   int32  `json:"bufferSize"`
}

func NewXrayConfig() *XrayConfig {
	return &XrayConfig{
		LogConfig: &XrayLogConfig{
			Level:      "warning",
			AccessPath: "",
			ErrorPath:  "",
		},
		AssetPath:          "/etc/V2bX/",
		DnsConfigPath:      "",
		InboundConfigPath:  "",
		OutboundConfigPath: "",
		RouteConfigPath:    "",
		ConnectionConfig: &XrayConnectionConfig{
			Handshake:    4,
			ConnIdle:     30,
			UplinkOnly:   2,
			DownlinkOnly: 4,
			BufferSize:   64,
		},
	}
}

func (c *XrayConfig) UnmarshalJSON(data []byte) error {
	type xrayConfig XrayConfig

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if err := json.Unmarshal(data, (*xrayConfig)(c)); err != nil {
		return err
	}
	_, c.outboundConfigPathSet = raw["OutboundConfigPath"]
	return nil
}

// ResolveOutboundConfigPath returns the custom outbound config file to load and
// whether it was detected next to AssetPath instead of being configured.
//
// An explicitly configured OutboundConfigPath is returned as is, including an
// empty one: only a config that does not mention the key at all opts into the
// sidecar file.
func (c *XrayConfig) ResolveOutboundConfigPath() (path string, detected bool) {
	if c.outboundConfigPathSet {
		return c.OutboundConfigPath, false
	}
	if c.AssetPath == "" {
		return "", false
	}

	sidecar := filepath.Join(c.AssetPath, "custom_outbound.json")
	info, err := os.Stat(sidecar)
	if err != nil || info.IsDir() {
		return "", false
	}

	return sidecar, true
}

type XrayOptions struct {
	EnableProxyProtocol bool                    `json:"EnableProxyProtocol"`
	EnableDNS           bool                    `json:"EnableDNS"`
	DNSType             string                  `json:"DNSType"`
	EnableUot           bool                    `json:"EnableUot"`
	EnableTFO           bool                    `json:"EnableTFO"`
	DisableIVCheck      bool                    `json:"DisableIVCheck"`
	DisableSniffing     bool                    `json:"DisableSniffing"`
	EnableFallback      bool                    `json:"EnableFallback"`
	FallBackConfigs     []FallBackConfigForXray `json:"FallBackConfigs"`
}

type FallBackConfigForXray struct {
	SNI              string `json:"SNI"`
	Alpn             string `json:"Alpn"`
	Path             string `json:"Path"`
	Dest             string `json:"Dest"`
	ProxyProtocolVer uint64 `json:"ProxyProtocolVer"`
}

func NewXrayOptions() *XrayOptions {
	return &XrayOptions{
		EnableProxyProtocol: false,
		EnableDNS:           false,
		DNSType:             "AsIs",
		EnableUot:           false,
		EnableTFO:           false,
		DisableIVCheck:      false,
		DisableSniffing:     false,
		EnableFallback:      false,
	}
}
