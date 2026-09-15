package panel

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/InazumaV/V2bX/conf"
	"github.com/go-resty/resty/v2"
)

// Panel is the interface for different panel's api.

type Client struct {
	// client retries transient failures, which is safe for the read only
	// endpoints and for the reports that replace a value (node status, online
	// devices).
	client *resty.Client
	// pushClient sends the traffic report without retrying: /push applies the
	// values as an increment on the panel side, so replaying a request whose
	// response was lost bills the user twice. A failed report is not lost either,
	// the controller puts the usage back and reports it on the next round.
	pushClient       *resty.Client
	PanelType        string
	APIHost          string
	Token            string
	NodeType         string
	NodeId           int
	nodeEtag         string
	userEtag         string
	responseBodyHash string
	UserList         *UserListBody
	AliveMap         *AliveMap
}

func New(c *conf.ApiConfig) (*Client, error) {
	// Check node type. It is normalized before the clients are built so both
	// send the same node_type.
	c.NodeType = strings.ToLower(c.NodeType)
	switch c.NodeType {
	case "v2ray":
		c.NodeType = "vmess"
	case
		"vmess",
		"trojan",
		"shadowsocks",
		"hysteria",
		"hysteria2",
		"tuic",
		"anytls",
		"naive",
		"vless":
	default:
		return nil, fmt.Errorf("unsupported Node type: %s", c.NodeType)
	}

	return &Client{
		client:     newPanelClient(c, true),
		pushClient: newPanelClient(c, false),
		PanelType:  c.PanelType,
		Token:      c.Key,
		APIHost:    c.APIHost,
		NodeType:   c.NodeType,
		NodeId:     c.NodeID,
		UserList:   &UserListBody{},
		AliveMap:   &AliveMap{},
	}, nil
}

// newPanelClient builds a panel client with the node credentials and timeout.
// Retrying is opt in because it must not be used for the endpoints that apply
// increments.
func newPanelClient(c *conf.ApiConfig, retry bool) *resty.Client {
	client := resty.New()
	if retry {
		client.SetRetryCount(3)
	}
	if c.Timeout > 0 {
		client.SetTimeout(time.Duration(c.Timeout) * time.Second)
	} else {
		client.SetTimeout(5 * time.Second)
	}
	client.OnError(func(req *resty.Request, err error) {
		var v *resty.ResponseError
		if errors.As(err, &v) {
			// v.Response contains the last response from the server
			// v.Err contains the original error
			logrus.Error(v.Err)
		}
	})
	client.SetBaseURL(c.APIHost)
	// set params
	switch c.PanelType {
	case "ppanel":
		{
			client.SetQueryParams(map[string]string{
				"protocol":   c.NodeType,
				"server_id":  strconv.Itoa(c.NodeID),
				"secret_key": c.Key,
			})
		}
	default:
		{
			client.SetQueryParams(map[string]string{
				"node_type": c.NodeType,
				"node_id":   strconv.Itoa(c.NodeID),
				"token":     c.Key,
			})
		}
	}
	return client
}
