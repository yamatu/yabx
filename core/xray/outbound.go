package xray

import (
	"fmt"
	"strings"

	conf2 "github.com/InazumaV/V2bX/conf"
	"github.com/goccy/go-json"
	"github.com/xtls/xray-core/core"
	"github.com/xtls/xray-core/infra/conf"
)

// BuildOutbound build freedom outbund config for addoutbound
func buildOutbound(config *conf2.Options, tag string) (*core.OutboundHandlerConfig, error) {
	outboundDetourConfig := &conf.OutboundDetourConfig{}
	outboundDetourConfig.Protocol = "freedom"
	outboundDetourConfig.Tag = tag

	// Build Send IP address
	if config.SendIP != "" {
		outboundDetourConfig.SendThrough = &config.SendIP
	}

	// xray-core deprecated freedom.domainStrategy in favour of sockopt.domainStrategy.
	if config.XrayOptions.EnableDNS {
		outboundDetourConfig.StreamSetting = &conf.StreamConfig{
			SocketSettings: &conf.SocketConfig{
				DomainStrategy: normalizeDomainStrategy(config.XrayOptions.DNSType),
			},
		}
	}
	var setting json.RawMessage
	setting, err := json.Marshal(&conf.FreedomConfig{})
	if err != nil {
		return nil, fmt.Errorf("marshal proxy config error: %s", err)
	}
	outboundDetourConfig.Settings = &setting
	return outboundDetourConfig.Build()
}

// normalizeDomainStrategy 兼容旧配置写法，转换为 xray-core 支持的取值
func normalizeDomainStrategy(input string) string {
	in := strings.ToLower(strings.TrimSpace(input))
	switch in {
	case "", "useip", "use_ip":
		return "UseIP"
	case "asis", "as_is", "as is":
		return "AsIs"
	case "ipv4_only", "useipv4", "use_ipv4", "ipv4":
		return "UseIPv4"
	case "ipv6_only", "useipv6", "use_ipv6", "ipv6":
		return "UseIPv6"
	case "prefer_ipv4", "preferipv4":
		return "PreferIPv4"
	case "prefer_ipv6", "preferipv6":
		return "PreferIPv6"
	default:
		// 如果是未知值，保持原样，交给核心报错
		return input
	}
}
