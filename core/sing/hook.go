package sing

import (
	"context"
	"fmt"
	"net"
	"sync"

	"github.com/InazumaV/V2bX/common/format"
	"github.com/InazumaV/V2bX/common/rate"

	"github.com/InazumaV/V2bX/limiter"

	"github.com/InazumaV/V2bX/common/counter"
	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/log"
	N "github.com/sagernet/sing/common/network"
)

var _ adapter.ConnectionTracker = (*HookServer)(nil)

type HookServer struct {
	counter sync.Map //map[string]*counter.TrafficCounter
}

func (h *HookServer) ModeList() []string {
	return nil
}

func NewHookServer() *HookServer {
	server := &HookServer{
		counter: sync.Map{},
	}
	return server
}

func (h *HookServer) RoutedConnection(_ context.Context, conn net.Conn, m adapter.InboundContext, _ adapter.Rule, _ adapter.Outbound) net.Conn {
	l, err := limiter.GetLimiter(m.Inbound)
	if err != nil {
		log.Warn("get limiter for ", m.Inbound, " error: ", err)
		return conn
	}
	taguuid := format.UserTag(m.Inbound, m.User)
	ip := m.Source.Addr.String()
	if b, r := l.CheckLimit(taguuid, ip, true, true); r {
		conn.Close()
		log.Error("[", m.Inbound, "] ", "Limited ", m.User, " by ip or conn")
		return conn
	} else if b != nil {
		conn = rate.NewConnRateLimiter(conn, b)
	}
	if l != nil {
		destStr := m.Destination.AddrString()
		protocol := m.Destination.Network()
		if l.CheckDomainRule(destStr) {
			log.Error(fmt.Sprintf(
				"User %s access domain %s reject by rule",
				m.User,
				destStr))
			conn.Close()
			return conn
		}
		if len(protocol) != 0 {
			if l.CheckProtocolRule(protocol) {
				log.Error(fmt.Sprintf(
					"User %s access protocol %s reject by rule",
					m.User,
					protocol))
				conn.Close()
				return conn
			}
		}
	}
	var t *counter.TrafficCounter
	if c, ok := h.counter.Load(m.Inbound); !ok {
		t = counter.NewTrafficCounter()
		h.counter.Store(m.Inbound, t)
	} else {
		t = c.(*counter.TrafficCounter)
	}
	conn = counter.NewConnCounter(conn, t.GetCounter(m.User))
	// Reference-count the online device for the whole life of the connection so
	// long lived connections stay online and disappear shortly after Close.
	l.Online.Add(taguuid, ip, l.UserID(taguuid))
	return newTrackedConn(conn, func() {
		l.Online.Del(taguuid, ip)
		// Release the tcp connection slot as well: sing never decremented it, so
		// the per-user connection count only ever grew and the user ended up
		// permanently rejected by ConnLimit.
		l.ConnLimiter.DelConnCount(taguuid, ip)
	})
}

func (h *HookServer) RoutedPacketConnection(_ context.Context, conn N.PacketConn, m adapter.InboundContext, _ adapter.Rule, _ adapter.Outbound) N.PacketConn {
	l, err := limiter.GetLimiter(m.Inbound)
	if err != nil {
		log.Warn("get limiter for ", m.Inbound, " error: ", err)
		return conn
	}
	ip := m.Source.Addr.String()
	taguuid := format.UserTag(m.Inbound, m.User)
	if b, r := l.CheckLimit(taguuid, ip, false, false); r {
		conn.Close()
		log.Error("[", m.Inbound, "] ", "Limited ", m.User, " by ip or conn")
		return conn
	} else if b != nil {
		//conn = rate.NewPacketConnCounter(conn, b)
	}
	if l != nil {
		destStr := m.Destination.AddrString()
		protocol := m.Destination.Network()
		if l.CheckDomainRule(destStr) {
			log.Error(fmt.Sprintf(
				"User %s access domain %s reject by rule",
				m.User,
				destStr))
			conn.Close()
			return conn
		}
		if len(protocol) != 0 {
			if l.CheckProtocolRule(protocol) {
				log.Error(fmt.Sprintf(
					"User %s access protocol %s reject by rule",
					m.User,
					protocol))
				conn.Close()
				return conn
			}
		}
	}
	var t *counter.TrafficCounter
	if c, ok := h.counter.Load(m.Inbound); !ok {
		t = counter.NewTrafficCounter()
		h.counter.Store(m.Inbound, t)
	} else {
		t = c.(*counter.TrafficCounter)
	}
	conn = counter.NewPacketConnCounter(conn, t.GetCounter(m.User))
	l.Online.Add(taguuid, ip, l.UserID(taguuid))
	return newTrackedPacketConn(conn, func() { l.Online.Del(taguuid, ip) })
}

type trackedConn struct {
	net.Conn
	once    sync.Once
	onClose func()
}

func newTrackedConn(conn net.Conn, onClose func()) net.Conn {
	return &trackedConn{Conn: conn, onClose: onClose}
}

func (c *trackedConn) Close() error {
	err := c.Conn.Close()
	c.once.Do(c.onClose)
	return err
}

type trackedPacketConn struct {
	N.PacketConn
	once    sync.Once
	onClose func()
}

func newTrackedPacketConn(conn N.PacketConn, onClose func()) N.PacketConn {
	return &trackedPacketConn{PacketConn: conn, onClose: onClose}
}

func (c *trackedPacketConn) Close() error {
	err := c.PacketConn.Close()
	c.once.Do(c.onClose)
	return err
}
