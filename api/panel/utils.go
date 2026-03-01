package panel

import (
	"fmt"
	"github.com/go-resty/resty/v2"
	"strings"
)

// Debug set the client debug for client
func (c *Client) Debug() {
	c.client.SetDebug(true)
}

func (c *Client) assembleURL(path string) string {
	base := strings.TrimRight(c.APIHost, "/")
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return base + path
}
func (c *Client) checkResponse(res *resty.Response, path string, err error) error {
	if err != nil {
		return fmt.Errorf("request %s failed: %s", c.assembleURL(path), err)
	}
	if res.StatusCode() >= 400 {
		body := res.Body()
		return fmt.Errorf("request %s failed: %s", c.assembleURL(path), string(body))
	}
	return nil
}
