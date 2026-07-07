package panel

import (
	"bytes"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"

	"github.com/go-resty/resty/v2"
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
	requestURL := c.assembleURL(path)
	if err != nil {
		return fmt.Errorf("request %s failed: %s", requestURL, describeRequestError(err))
	}
	if res == nil {
		return fmt.Errorf("request %s failed: nil response", requestURL)
	}
	if res.StatusCode() >= 400 {
		return fmt.Errorf("request %s failed: http %d: %s", requestURL, res.StatusCode(), summarizeResponseBody(res.Body()))
	}
	return nil
}

func describeRequestError(err error) string {
	if err == nil {
		return ""
	}

	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return fmt.Sprintf("dns lookup failed for %s: %v", dnsErr.Name, dnsErr.Err)
	}

	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return fmt.Sprintf("network timeout: %v", err)
	}

	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return fmt.Sprintf("%s %s failed: %s", urlErr.Op, urlErr.URL, classifyTransportError(urlErr.Err))
	}

	return classifyTransportError(err)
}

func classifyTransportError(err error) string {
	if err == nil {
		return ""
	}

	var opErr *net.OpError
	if errors.As(err, &opErr) {
		if opErr.Addr != nil {
			return fmt.Sprintf("%s %s failed: %s", opErr.Op, opErr.Addr.String(), classifyTransportError(opErr.Err))
		}
		return fmt.Sprintf("%s failed: %s", opErr.Op, classifyTransportError(opErr.Err))
	}

	var syscallErr *os.SyscallError
	if errors.As(err, &syscallErr) {
		msg := strings.ToLower(syscallErr.Err.Error())
		switch {
		case strings.Contains(msg, "connection refused"):
			return "tcp connection refused; check panel host, port, firewall, and reverse proxy listener"
		case strings.Contains(msg, "no route to host"):
			return "no route to host; check server network route or firewall"
		case strings.Contains(msg, "network is unreachable"):
			return "network unreachable; check IPv4/IPv6 connectivity"
		}
		return syscallErr.Err.Error()
	}

	msg := err.Error()
	lower := strings.ToLower(msg)
	switch {
	case strings.Contains(lower, "no such host"):
		return "dns lookup failed; check panel domain DNS records"
	case strings.Contains(lower, "connection refused"):
		return "tcp connection refused; check panel port, firewall, and reverse proxy listener"
	case strings.Contains(lower, "i/o timeout") || strings.Contains(lower, "deadline exceeded"):
		return "network timeout; check panel reachability, port, and firewall"
	case strings.Contains(lower, "certificate") || strings.Contains(lower, "tls"):
		return "tls/certificate error; check panel HTTPS certificate and ApiHost scheme"
	}
	return msg
}

func summarizeResponseBody(body []byte) string {
	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		return "<empty body>"
	}
	const max = 512
	if len(body) > max {
		return string(body[:max]) + "...(truncated)"
	}
	return string(body)
}
