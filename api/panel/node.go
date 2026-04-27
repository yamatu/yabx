package panel

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/goccy/go-json"
)

// Security type
const (
	None    = 0
	Tls     = 1
	Reality = 2
)

type NodeInfo struct {
	Id           int
	Type         string
	Protocol     string
	Security     int
	PushInterval time.Duration
	PullInterval time.Duration
	RawDNS       RawDNS
	Rules        Rules

	Config json.RawMessage `json:"config"`

	// origin
	VAllss      *VAllssNode
	Shadowsocks *ShadowsocksNode
	Trojan      *TrojanNode
	Tuic        *TuicNode
	AnyTLS      *AnyTLSNode
	Naive       *NaiveNode
	Hysteria    *HysteriaNode
	Hysteria2   *Hysteria2Node
	Common      *CommonNode
	Basic       *BasicConfig `json:"basic"`
}

type BasicConfig struct {
	PushInterval any `json:"push_interval"`
	PullInterval any `json:"pull_interval"`
}

type CommonNode struct {
	Host       string      `json:"host"`
	ServerPort int         `json:"server_port"`
	ServerName string      `json:"server_name"`
	Routes     []Route     `json:"routes"`
	BaseConfig *BaseConfig `json:"base_config"`
}

type Route struct {
	Id          int         `json:"id"`
	Match       interface{} `json:"match"`
	Action      string      `json:"action"`
	ActionValue string      `json:"action_value"`
}
type BaseConfig struct {
	PushInterval any `json:"push_interval"`
	PullInterval any `json:"pull_interval"`
}

// VAllssNode is vmess and vless node info
type VAllssNode struct {
	CommonNode
	Tls                 int             `json:"tls"`
	TlsSettings         TlsSettings     `json:"tls_settings"`
	TlsSettingsBack     *TlsSettings    `json:"tlsSettings"`
	Network             string          `json:"network"`
	NetworkSettings     json.RawMessage `json:"network_settings"`
	NetworkSettingsBack json.RawMessage `json:"networkSettings"`
	ServerName          string          `json:"server_name"`

	// vless only
	Encryption    string        `json:"encryption"`
	Decryption    string        `json:"decryption"`
	Flow          string        `json:"flow"`
	RealityConfig RealityConfig `json:"-"`
}

func (v *VAllssNode) NormalizedEncryption() string {
	if v == nil {
		return ""
	}
	return strings.TrimSpace(v.Encryption)
}

func (v *VAllssNode) NormalizedDecryption() string {
	if v == nil {
		return ""
	}
	return strings.TrimSpace(v.Decryption)
}

func (v *VAllssNode) HasVlessEncryption() bool {
	if v == nil {
		return false
	}
	return v.NormalizedEncryption() != "" && v.NormalizedDecryption() != ""
}

type TlsSettings struct {
	ServerName string      `json:"server_name"`
	Dest       string      `json:"dest"`
	ServerPort string      `json:"server_port"`
	ShortId    string      `json:"short_id"`
	PrivateKey string      `json:"private_key"`
	Xver       uint64      `json:"xver,string"`
	ECH        ECHSettings `json:"ech"`
}

type ECHSettings struct {
	Enabled         bool   `json:"enabled"`
	Config          string `json:"config"`
	ConfigList      string `json:"config_list"`
	ForceQuery      string `json:"force_query"`
	QueryServerName string `json:"query_server_name"`
	PrivateKey      string `json:"private_key"`
	ServerKeys      string `json:"server_keys"`
}

type tlsSettingsJSON struct {
	ServerName string          `json:"server_name"`
	Dest       string          `json:"dest"`
	ServerPort json.RawMessage `json:"server_port"`
	ShortId    string          `json:"short_id"`
	PrivateKey string          `json:"private_key"`
	Xver       uint64          `json:"xver,string"`
	ECH        ECHSettings     `json:"ech"`
}

func (t *TlsSettings) UnmarshalJSON(data []byte) error {
	var aux tlsSettingsJSON
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	serverPort, err := decodeStringOrNumber(aux.ServerPort)
	if err != nil {
		return fmt.Errorf("decode tls server_port error: %w", err)
	}

	t.ServerName = aux.ServerName
	t.Dest = aux.Dest
	t.ServerPort = serverPort
	t.ShortId = aux.ShortId
	t.PrivateKey = aux.PrivateKey
	t.Xver = aux.Xver
	t.ECH = aux.ECH
	return nil
}

func decodeStringOrNumber(raw json.RawMessage) (string, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return "", nil
	}

	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return strings.TrimSpace(s), nil
	}

	var i int64
	if err := json.Unmarshal(raw, &i); err == nil {
		return strconv.FormatInt(i, 10), nil
	}

	var u uint64
	if err := json.Unmarshal(raw, &u); err == nil {
		return strconv.FormatUint(u, 10), nil
	}

	var f float64
	if err := json.Unmarshal(raw, &f); err == nil {
		return strconv.FormatFloat(f, 'f', -1, 64), nil
	}

	return "", fmt.Errorf("unsupported value: %s", string(raw))
}

type RealityConfig struct {
	Xver         uint64 `json:"Xver"`
	MinClientVer string `json:"MinClientVer"`
	MaxClientVer string `json:"MaxClientVer"`
	MaxTimeDiff  string `json:"MaxTimeDiff"`
}

type ShadowsocksNode struct {
	CommonNode
	Cipher    string `json:"cipher"`
	ServerKey string `json:"server_key"`
}

type TrojanNode struct {
	CommonNode
	Network             string          `json:"network"`
	NetworkSettings     json.RawMessage `json:"networkSettings"`
	NetworkSettingsBack json.RawMessage `json:"network_settings"`
	TlsSettings         TlsSettings     `json:"tls_settings"`
	TlsSettingsBack     *TlsSettings    `json:"tlsSettings"`
}

type TuicNode struct {
	CommonNode
	CongestionControl string `json:"congestion_control"`
	ZeroRTTHandshake  bool   `json:"zero_rtt_handshake"`
}

type AnyTLSNode struct {
	CommonNode
	PaddingScheme json.RawMessage `json:"padding_scheme,omitempty"`

	Tls             int           `json:"tls"`
	TlsSettings     TlsSettings   `json:"tls_settings"`
	TlsSettingsBack *TlsSettings  `json:"tlsSettings"`
	RealityConfig   RealityConfig `json:"-"`
}

type NaiveNode struct {
	CommonNode
	Network             string          `json:"network"`
	NetworkSettings     json.RawMessage `json:"network_settings"`
	NetworkSettingsBack json.RawMessage `json:"networkSettings"`
	Tls                 int             `json:"tls"`
	TlsSettings         TlsSettings     `json:"tls_settings"`
	TlsSettingsBack     *TlsSettings    `json:"tlsSettings"`
}

type HysteriaNode struct {
	CommonNode
	UpMbps   int    `json:"up_mbps"`
	DownMbps int    `json:"down_mbps"`
	Obfs     string `json:"obfs"`
}

type Hysteria2Node struct {
	CommonNode
	Ignore_Client_Bandwidth bool   `json:"ignore_client_bandwidth"`
	UpMbps                  int    `json:"up_mbps"`
	DownMbps                int    `json:"down_mbps"`
	ObfsType                string `json:"obfs"`
	ObfsPassword            string `json:"obfs-password"`
}

type RawDNS struct {
	DNSMap  map[string]map[string]interface{}
	DNSJson []byte
}

type Rules struct {
	Regexp   []string
	Protocol []string
}

type ServerPushStatusRequest struct {
	Cpu       float64 `json:"cpu"`
	Mem       float64 `json:"mem"`
	Disk      float64 `json:"disk"`
	UpdatedAt int64   `json:"updated_at"`
}

type NodeStatus struct {
	CPU       float64
	Mem       float64
	Disk      float64
	Uptime    uint64
	MemTotal  uint64
	MemUsed   uint64
	SwapTotal uint64
	SwapUsed  uint64
	DiskTotal uint64
	DiskUsed  uint64
}

func (c *Client) GetNodeInfo() (node *NodeInfo, err error) {
	switch c.PanelType {
	case "ppanel":
		{
			const path = "/v1/server/config"
			r, err := c.client.
				R().
				SetHeader("If-None-Match", c.nodeEtag).
				ForceContentType("application/json").
				Get(path)

			if r.StatusCode() == 304 {
				return nil, nil
			}
			hash := sha256.Sum256(r.Body())
			newBodyHash := hex.EncodeToString(hash[:])
			if c.responseBodyHash == newBodyHash {
				return nil, nil
			}
			c.responseBodyHash = newBodyHash
			c.nodeEtag = r.Header().Get("ETag")
			if err = c.checkResponse(r, path, err); err != nil {
				return nil, err
			}

			if r != nil {
				defer func() {
					if r.RawBody() != nil {
						r.RawBody().Close()
					}
				}()
			} else {
				return nil, fmt.Errorf("received nil response")
			}
			node = &NodeInfo{
				Id:     c.NodeId,
				Type:   c.NodeType,
				Common: &CommonNode{},
			}
			// parse protocol params
			err = json.Unmarshal(r.Body(), node)
			if err != nil {
				return nil, fmt.Errorf("decode node params error: %s", err)
			}
			// set interval
			node.PushInterval = intervalToTime(node.Basic.PushInterval)
			node.PullInterval = intervalToTime(node.Basic.PullInterval)
			node.Type = node.Protocol
			switch node.Protocol {
			case "vmess", "vless":
				node.VAllss = &VAllssNode{}
				err = json.Unmarshal(node.Config, node.VAllss)
			case "trojan":
				node.Trojan = &TrojanNode{}
				err = json.Unmarshal(node.Config, node.Trojan)
			case "shadowsocks":
				node.Shadowsocks = &ShadowsocksNode{}
				err = json.Unmarshal(node.Config, node.Shadowsocks)
			case "tuic":
				node.Tuic = &TuicNode{}
				err = json.Unmarshal(node.Config, node.Tuic)
			case "hysteria2":
				node.Hysteria2 = &Hysteria2Node{}
				err = json.Unmarshal(node.Config, node.Hysteria2)
			case "anytls":
				node.AnyTLS = &AnyTLSNode{}
				err = json.Unmarshal(node.Config, node.AnyTLS)
			case "naive":
				node.Naive = &NaiveNode{}
				err = json.Unmarshal(node.Config, node.Naive)
			default:
				err = fmt.Errorf("unknown protocol:%s", node.Protocol)
			}

			switch node.Protocol {
			case "vmess", "vless":
				if node.VAllss.TlsSettingsBack != nil {
					node.VAllss.TlsSettings = *node.VAllss.TlsSettingsBack
					node.VAllss.TlsSettingsBack = nil
				}
				node.Security = node.VAllss.Tls
			case "trojan":
				if len(node.Trojan.NetworkSettingsBack) > 0 {
					node.Trojan.NetworkSettings = node.Trojan.NetworkSettingsBack
					node.Trojan.NetworkSettingsBack = nil
				}
				if node.Trojan.TlsSettingsBack != nil {
					node.Trojan.TlsSettings = *node.Trojan.TlsSettingsBack
					node.Trojan.TlsSettingsBack = nil
				}
				node.Security = Tls
			case "anytls":
				if node.AnyTLS.TlsSettingsBack != nil {
					node.AnyTLS.TlsSettings = *node.AnyTLS.TlsSettingsBack
					node.AnyTLS.TlsSettingsBack = nil
				}
				node.Security = node.AnyTLS.Tls
			case "naive":
				if node.Naive.TlsSettingsBack != nil {
					node.Naive.TlsSettings = *node.Naive.TlsSettingsBack
					node.Naive.TlsSettingsBack = nil
				}
				node.Security = node.Naive.Tls
			}

			if err != nil {
				return nil, fmt.Errorf("decode node config error: %s", err)
			}

			return node, nil
		}
	default:
		{
			const path = "/api/v1/server/UniProxy/config"
			r, err := c.client.
				R().
				SetHeader("If-None-Match", c.nodeEtag).
				ForceContentType("application/json").
				Get(path)

			if r.StatusCode() == 304 {
				return nil, nil
			}
			hash := sha256.Sum256(r.Body())
			newBodyHash := hex.EncodeToString(hash[:])
			if c.responseBodyHash == newBodyHash {
				return nil, nil
			}
			c.responseBodyHash = newBodyHash
			c.nodeEtag = r.Header().Get("ETag")
			if err = c.checkResponse(r, path, err); err != nil {
				return nil, err
			}

			if r != nil {
				defer func() {
					if r.RawBody() != nil {
						r.RawBody().Close()
					}
				}()
			} else {
				return nil, fmt.Errorf("received nil response")
			}
			node = &NodeInfo{
				Id:   c.NodeId,
				Type: c.NodeType,
				RawDNS: RawDNS{
					DNSMap:  make(map[string]map[string]interface{}),
					DNSJson: []byte(""),
				},
			}
			// parse protocol params
			var cm *CommonNode
			switch c.NodeType {
			case "vmess", "vless":
				rsp := &VAllssNode{}
				err = json.Unmarshal(r.Body(), rsp)
				if err != nil {
					return nil, fmt.Errorf("decode v2ray params error: %s", err)
				}
				if len(rsp.NetworkSettingsBack) > 0 {
					rsp.NetworkSettings = rsp.NetworkSettingsBack
					rsp.NetworkSettingsBack = nil
				}
				if rsp.TlsSettingsBack != nil {
					rsp.TlsSettings = *rsp.TlsSettingsBack
					rsp.TlsSettingsBack = nil
				}
				cm = &rsp.CommonNode
				node.VAllss = rsp
				node.Security = node.VAllss.Tls
			case "shadowsocks":
				rsp := &ShadowsocksNode{}
				err = json.Unmarshal(r.Body(), rsp)
				if err != nil {
					return nil, fmt.Errorf("decode shadowsocks params error: %s", err)
				}
				cm = &rsp.CommonNode
				node.Shadowsocks = rsp
				node.Security = None
			case "trojan":
				rsp := &TrojanNode{}
				err = json.Unmarshal(r.Body(), rsp)
				if err != nil {
					return nil, fmt.Errorf("decode trojan params error: %s", err)
				}
				if len(rsp.NetworkSettingsBack) > 0 {
					rsp.NetworkSettings = rsp.NetworkSettingsBack
					rsp.NetworkSettingsBack = nil
				}
				if rsp.TlsSettingsBack != nil {
					rsp.TlsSettings = *rsp.TlsSettingsBack
					rsp.TlsSettingsBack = nil
				}
				cm = &rsp.CommonNode
				node.Trojan = rsp
				node.Security = Tls
			case "tuic":
				rsp := &TuicNode{}
				err = json.Unmarshal(r.Body(), rsp)
				if err != nil {
					return nil, fmt.Errorf("decode tuic params error: %s", err)
				}
				cm = &rsp.CommonNode
				node.Tuic = rsp
				node.Security = Tls
			case "anytls":
				rsp := &AnyTLSNode{}
				err = json.Unmarshal(r.Body(), rsp)
				if err != nil {
					return nil, fmt.Errorf("decode anytls params error: %s", err)
				}
				if rsp.TlsSettingsBack != nil {
					rsp.TlsSettings = *rsp.TlsSettingsBack
					rsp.TlsSettingsBack = nil
				}
				cm = &rsp.CommonNode
				node.AnyTLS = rsp
				node.Security = node.AnyTLS.Tls
			case "naive":
				rsp := &NaiveNode{}
				err = json.Unmarshal(r.Body(), rsp)
				if err != nil {
					return nil, fmt.Errorf("decode naive params error: %s", err)
				}
				if rsp.TlsSettingsBack != nil {
					rsp.TlsSettings = *rsp.TlsSettingsBack
					rsp.TlsSettingsBack = nil
				}
				cm = &rsp.CommonNode
				node.Naive = rsp
				node.Security = node.Naive.Tls
			case "hysteria":
				rsp := &HysteriaNode{}
				err = json.Unmarshal(r.Body(), rsp)
				if err != nil {
					return nil, fmt.Errorf("decode hysteria params error: %s", err)
				}
				cm = &rsp.CommonNode
				node.Hysteria = rsp
				node.Security = Tls
			case "hysteria2":
				rsp := &Hysteria2Node{}
				err = json.Unmarshal(r.Body(), rsp)
				if err != nil {
					return nil, fmt.Errorf("decode hysteria2 params error: %s", err)
				}
				cm = &rsp.CommonNode
				node.Hysteria2 = rsp
				node.Security = Tls
			}

			// parse rules and dns
			for i := range cm.Routes {
				var matchs []string
				if _, ok := cm.Routes[i].Match.(string); ok {
					matchs = strings.Split(cm.Routes[i].Match.(string), ",")
				} else if _, ok = cm.Routes[i].Match.([]string); ok {
					matchs = cm.Routes[i].Match.([]string)
				} else {
					temp := cm.Routes[i].Match.([]interface{})
					matchs = make([]string, len(temp))
					for i := range temp {
						matchs[i] = temp[i].(string)
					}
				}
				switch cm.Routes[i].Action {
				case "block":
					for _, v := range matchs {
						if strings.HasPrefix(v, "protocol:") {
							// protocol
							node.Rules.Protocol = append(node.Rules.Protocol, strings.TrimPrefix(v, "protocol:"))
						} else {
							// domain
							node.Rules.Regexp = append(node.Rules.Regexp, strings.TrimPrefix(v, "regexp:"))
						}
					}
				case "dns":
					var domains []string
					domains = append(domains, matchs...)
					if matchs[0] != "main" {
						node.RawDNS.DNSMap[strconv.Itoa(i)] = map[string]interface{}{
							"address": cm.Routes[i].ActionValue,
							"domains": domains,
						}
					} else {
						dns := []byte(strings.Join(matchs[1:], ""))
						node.RawDNS.DNSJson = dns
					}
				}
			}

			// set interval
			node.PushInterval = intervalToTime(cm.BaseConfig.PushInterval)
			node.PullInterval = intervalToTime(cm.BaseConfig.PullInterval)

			node.Common = cm
			// clear
			cm.Routes = nil
			cm.BaseConfig = nil

			return node, nil
		}

	}

}

func intervalToTime(i interface{}) time.Duration {
	switch reflect.TypeOf(i).Kind() {
	case reflect.Int:
		return time.Duration(i.(int)) * time.Second
	case reflect.String:
		i, _ := strconv.Atoi(i.(string))
		return time.Duration(i) * time.Second
	case reflect.Float64:
		return time.Duration(i.(float64)) * time.Second
	default:
		return time.Duration(reflect.ValueOf(i).Int()) * time.Second
	}
}

func (c *Client) ReportNodeStatus(nodeStatus *NodeStatus) (err error) {
	switch c.PanelType {
	case "ppanel":
		path := "/v1/server/status"
		status := ServerPushStatusRequest{
			Cpu:       nodeStatus.CPU,
			Mem:       nodeStatus.Mem,
			Disk:      nodeStatus.Disk,
			UpdatedAt: time.Now().UnixMilli(),
		}
		if _, err = c.client.R().SetBody(status).ForceContentType("application/json").Post(path); err != nil {
			return fmt.Errorf("request %s failed: %v", c.assembleURL(path), err.Error())
		}
		return nil
	default:
		payload := map[string]interface{}{
			"cpu": nodeStatus.CPU,
			"mem": map[string]uint64{
				"total": nodeStatus.MemTotal,
				"used":  nodeStatus.MemUsed,
			},
			"swap": map[string]uint64{
				"total": nodeStatus.SwapTotal,
				"used":  nodeStatus.SwapUsed,
			},
			"disk": map[string]uint64{
				"total": nodeStatus.DiskTotal,
				"used":  nodeStatus.DiskUsed,
			},
		}

		paths := []string{
			"/api/v1/server/UniProxy/status",
			"/api/v2/server/status",
		}
		var lastErr error
		for _, path := range paths {
			r, reqErr := c.client.R().SetBody(payload).ForceContentType("application/json").Post(path)
			if err = c.checkResponse(r, path, reqErr); err == nil {
				return nil
			}
			lastErr = err
		}

		return lastErr
	}

}
