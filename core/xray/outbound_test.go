package xray

import (
	"testing"

	conf2 "github.com/InazumaV/V2bX/conf"
	"github.com/xtls/xray-core/app/proxyman"
	"github.com/xtls/xray-core/proxy/freedom"
	"github.com/xtls/xray-core/transport/internet"
)

func buildTestOutbound(t *testing.T, enableDNS bool, dnsType string) (*proxyman.SenderConfig, *freedom.Config) {
	t.Helper()
	opt := &conf2.Options{XrayOptions: &conf2.XrayOptions{EnableDNS: enableDNS, DNSType: dnsType}}
	ob, err := buildOutbound(opt, "test")
	if err != nil {
		t.Fatalf("buildOutbound: %v", err)
	}
	sender, err := ob.SenderSettings.GetInstance()
	if err != nil {
		t.Fatalf("sender settings: %v", err)
	}
	proxy, err := ob.ProxySettings.GetInstance()
	if err != nil {
		t.Fatalf("proxy settings: %v", err)
	}
	return sender.(*proxyman.SenderConfig), proxy.(*freedom.Config)
}

// xray-core deprecated freedom.domainStrategy, so the strategy belongs on sockopt.
func TestBuildOutboundPutsTheDNSStrategyOnSockopt(t *testing.T) {
	sender, fc := buildTestOutbound(t, true, "UseIPv4")
	if fc.DomainStrategy != internet.DomainStrategy_AS_IS {
		t.Fatalf("freedom domainStrategy = %v, want AS_IS", fc.DomainStrategy)
	}
	if sender.StreamSettings == nil || sender.StreamSettings.SocketSettings == nil {
		t.Fatal("sockopt not set")
	}
	if got := sender.StreamSettings.SocketSettings.DomainStrategy; got != internet.DomainStrategy_USE_IP4 {
		t.Fatalf("sockopt domainStrategy = %v, want USE_IP4", got)
	}
}

func TestBuildOutboundWithoutDNSLeavesTheStrategyAlone(t *testing.T) {
	sender, fc := buildTestOutbound(t, false, "UseIPv4")
	if fc.DomainStrategy != internet.DomainStrategy_AS_IS {
		t.Fatalf("freedom domainStrategy = %v, want AS_IS", fc.DomainStrategy)
	}
	if ss := sender.StreamSettings; ss != nil && ss.SocketSettings != nil &&
		ss.SocketSettings.DomainStrategy != internet.DomainStrategy_AS_IS {
		t.Fatalf("sockopt domainStrategy = %v, want AS_IS", ss.SocketSettings.DomainStrategy)
	}
}
