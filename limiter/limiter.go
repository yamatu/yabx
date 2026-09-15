package limiter

import (
	"errors"
	"regexp"
	"sync"
	"time"

	"github.com/InazumaV/V2bX/api/panel"
	"github.com/InazumaV/V2bX/common/format"
	"github.com/InazumaV/V2bX/common/netutil"
	"github.com/InazumaV/V2bX/conf"
	"github.com/juju/ratelimit"
	log "github.com/sirupsen/logrus"
	"github.com/xtls/xray-core/common/task"
)

var limitLock sync.RWMutex
var limiter map[string]*Limiter

func Init() {
	limiter = map[string]*Limiter{}
	c := task.Periodic{
		Interval: time.Minute,
		Execute:  ClearOnlineIP,
	}
	go func() {
		log.WithField("Type", "Limiter").
			Debug("ClearOnlineIP started")
		time.Sleep(time.Minute)
		_ = c.Start()
	}()
}

type Limiter struct {
	DomainRules   []*regexp.Regexp
	ProtocolRules []string
	SpeedLimit    int
	UserOnlineIP  *sync.Map // Key: Name, value: {Key: Ip, value: Uid}

	// OldUserOnline remembers which IPs were online in the previous device
	// window (Key: Ip, value: oldOnlineEntry), so that a device that reconnects
	// is not counted as a new device by the device limit. Entries expire after
	// oldOnlineTTL.
	OldUserOnline *sync.Map
	UserLimitInfo *sync.Map    // Key: Uid value: UserLimitInfo
	ConnLimiter   *ConnLimiter // Key: Uid value: ConnLimiter
	SpeedLimiter  *sync.Map    // key: Uid, value: *speedBucket
	Online        *OnlineRegistry
	aliveMu       sync.RWMutex
	AliveList     map[int]int // Key: Uid, value: alive_ip
	oldOnlineTTL  time.Duration
}

// defaultOldOnlineTTL is how long an IP is remembered as "was online".
//
// The entry only has to survive one device window (the interval between two
// GetOnlineDevice calls, which is the panel push interval) so that a
// continuously online device is not counted twice. It is kept noticeably
// longer than the panel keeps its own device list (XBoard forgets a device
// after 300s without traffic): while the panel still counts a device, dropping
// the entry would reject the user's own reconnection as "one device too many".
const defaultOldOnlineTTL = 10 * time.Minute

type oldOnlineEntry struct {
	uid    int
	seenAt time.Time
}

type speedBucket struct {
	Limit  int64
	Bucket *ratelimit.Bucket
}

type UserLimitInfo struct {
	UID               int
	SpeedLimit        int
	DeviceLimit       int
	DynamicSpeedLimit int
	ExpireTime        int64
	OverLimit         bool
}

func AddLimiter(tag string, l *conf.LimitConfig, users []panel.UserInfo, aliveList map[int]int) *Limiter {
	info := &Limiter{
		SpeedLimit:    l.SpeedLimit,
		UserOnlineIP:  new(sync.Map),
		UserLimitInfo: new(sync.Map),
		ConnLimiter:   NewConnLimiter(l.ConnLimit, l.IPLimit, l.EnableRealtime, onlineTTLFromSeconds(l.OnlineTimeout)),
		SpeedLimiter:  new(sync.Map),
		Online:        newOnlineRegistry(onlineTTLFromSeconds(l.OnlineTimeout)),
		AliveList:     make(map[int]int),
		OldUserOnline: new(sync.Map),
		oldOnlineTTL:  defaultOldOnlineTTL,
	}
	info.SetAliveList(aliveList)
	for i := range users {
		userLimit := &UserLimitInfo{}
		userLimit.UID = users[i].Id
		if users[i].SpeedLimit != 0 {
			userLimit.SpeedLimit = users[i].SpeedLimit
		}
		if users[i].DeviceLimit != 0 {
			userLimit.DeviceLimit = users[i].DeviceLimit
		}
		userLimit.OverLimit = false
		info.UserLimitInfo.Store(format.UserTag(tag, users[i].Uuid), userLimit)
	}
	limitLock.Lock()
	if limiter == nil {
		limiter = map[string]*Limiter{}
	}
	limiter[tag] = info
	limitLock.Unlock()
	return info
}

func GetLimiter(tag string) (info *Limiter, err error) {
	limitLock.RLock()
	info, ok := limiter[tag]
	limitLock.RUnlock()
	if !ok {
		return nil, errors.New("not found")
	}
	return info, nil
}

func DeleteLimiter(tag string) {
	limitLock.Lock()
	delete(limiter, tag)
	limitLock.Unlock()
}

func (l *Limiter) UpdateUser(tag string, added []panel.UserInfo, deleted []panel.UserInfo) {
	for i := range deleted {
		l.UserLimitInfo.Delete(format.UserTag(tag, deleted[i].Uuid))
		l.UserOnlineIP.Delete(format.UserTag(tag, deleted[i].Uuid))
		l.SpeedLimiter.Delete(format.UserTag(tag, deleted[i].Uuid))
		l.DeleteAlive(deleted[i].Id)
		l.Online.DeleteUser(format.UserTag(tag, deleted[i].Uuid))
	}
	for i := range added {
		userLimit := &UserLimitInfo{
			UID: added[i].Id,
		}
		if added[i].SpeedLimit != 0 {
			userLimit.SpeedLimit = added[i].SpeedLimit
			userLimit.ExpireTime = 0
		}
		if added[i].DeviceLimit != 0 {
			userLimit.DeviceLimit = added[i].DeviceLimit
		}
		userLimit.OverLimit = false
		l.UserLimitInfo.Store(format.UserTag(tag, added[i].Uuid), userLimit)
	}
}

func (l *Limiter) UpdateDynamicSpeedLimit(tag, uuid string, limit int, expire time.Time) error {
	taguuid := format.UserTag(tag, uuid)
	if err := l.updateUserLimit(taguuid, func(u *UserLimitInfo) {
		u.DynamicSpeedLimit = limit
		u.ExpireTime = expire.Unix()
	}); err != nil {
		return err
	}
	l.SpeedLimiter.Delete(taguuid)
	return nil
}

// SetOverLimit flags the user as having hit a limit, so that the traffic of the
// rejected connections is not accounted (hysteria2 sets it on a rejected
// connection and clears it on the next traffic log).
func (l *Limiter) SetOverLimit(taguuid string, over bool) {
	_ = l.updateUserLimit(taguuid, func(u *UserLimitInfo) {
		u.OverLimit = over
	})
}

// ConsumeOverLimit reports whether the user is over the limit and clears the
// flag at the same time, so that only the first traffic log after a rejected
// connection is skipped.
func (l *Limiter) ConsumeOverLimit(taguuid string) bool {
	for {
		current, ok := l.UserLimitInfo.Load(taguuid)
		if !ok {
			return false
		}
		old, ok := current.(*UserLimitInfo)
		if !ok || !old.OverLimit {
			return false
		}
		updated := *old
		updated.OverLimit = false
		if l.UserLimitInfo.CompareAndSwap(taguuid, current, &updated) {
			return true
		}
	}
}

// updateUserLimit applies mutate to a copy of the user's limit info and swaps
// the copy in.
//
// The limit info is read on the connection hot path, so it is never mutated in
// place: the panel updates it (AddUsers/AddDynamicSpeedLimit) and hysteria2
// flips OverLimit from the connection callbacks, and a reader must never observe
// a half written entry.
func (l *Limiter) updateUserLimit(taguuid string, mutate func(*UserLimitInfo)) error {
	for {
		current, ok := l.UserLimitInfo.Load(taguuid)
		if !ok {
			return errors.New("not found")
		}
		old, ok := current.(*UserLimitInfo)
		if !ok {
			return errors.New("not found")
		}
		updated := *old
		mutate(&updated)
		if l.UserLimitInfo.CompareAndSwap(taguuid, current, &updated) {
			return nil
		}
	}
}

func (l *Limiter) SetAliveList(aliveList map[int]int) {
	l.aliveMu.Lock()
	defer l.aliveMu.Unlock()

	l.AliveList = make(map[int]int, len(aliveList))
	for uid, count := range aliveList {
		l.AliveList[uid] = count
	}
}

func (l *Limiter) DeleteAlive(uid int) {
	l.aliveMu.Lock()
	defer l.aliveMu.Unlock()

	delete(l.AliveList, uid)
}

func (l *Limiter) GetAlive(uid int) int {
	l.aliveMu.RLock()
	defer l.aliveMu.RUnlock()

	return l.AliveList[uid]
}

// UserID resolves the panel uid bound to a "tag|uuid" key.
func (l *Limiter) UserID(taguuid string) int {
	if v, ok := l.UserLimitInfo.Load(taguuid); ok {
		if u, ok := v.(*UserLimitInfo); ok {
			return u.UID
		}
	}
	return 0
}

// SetOnlineTTL updates how long an unreferenced online device is remembered.
// Cores that call Add/Del keep referenced devices online regardless of the TTL.
func (l *Limiter) SetOnlineTTL(ttl time.Duration) {
	l.Online.SetTTL(ttl)
	l.ConnLimiter.SetOnlineTTL(ttl)
}

// CheckLimit enforces speed/conn/device limits and records the device as online.
func (l *Limiter) CheckLimit(taguuid string, ip string, isTcp bool, noSSUDP bool) (Bucket *ratelimit.Bucket, Reject bool) {
	bucket, reject := l.checkLimit(taguuid, ip, isTcp, noSSUDP)
	if !reject {
		// A device is online as soon as one of its connections passes the
		// limits. Cores with a connection lifecycle additionally reference-count
		// the entry via Online.Add/Online.Del.
		l.Online.Touch(taguuid, ip, l.UserID(taguuid))
	}
	return bucket, reject
}

func (l *Limiter) checkLimit(taguuid string, ip string, isTcp bool, noSSUDP bool) (Bucket *ratelimit.Bucket, Reject bool) {
	ip = netutil.NormalizeIP(ip)

	// ip and conn limiter
	if l.ConnLimiter.AddConnCount(taguuid, ip, isTcp) {
		return nil, true
	}
	// check and gen speed limit Bucket
	nodeLimit := l.SpeedLimit
	userLimit := 0
	deviceLimit := 0
	var uid int
	if v, ok := l.UserLimitInfo.Load(taguuid); ok {
		u, ok := v.(*UserLimitInfo)
		if !ok {
			return nil, true
		}
		deviceLimit = u.DeviceLimit
		uid = u.UID
		if u.ExpireTime < time.Now().Unix() && u.ExpireTime != 0 {
			if u.SpeedLimit != 0 {
				userLimit = u.SpeedLimit
				// Drop the expired dynamic limit instead of updating the entry in
				// place: other goroutines read it while this connection is handled.
				_ = l.updateUserLimit(taguuid, func(u *UserLimitInfo) {
					u.DynamicSpeedLimit = 0
					u.ExpireTime = 0
				})
			} else {
				l.UserLimitInfo.Delete(taguuid)
			}
		} else {
			userLimit = determineSpeedLimit(u.SpeedLimit, u.DynamicSpeedLimit)
		}
	} else {
		return nil, true
	}
	if noSSUDP {
		// Store online user for device limit
		ipMap := new(sync.Map)
		ipMap.Store(ip, uid)
		aliveIp := l.GetAlive(uid)
		// If any device is online
		if v, ok := l.UserOnlineIP.LoadOrStore(taguuid, ipMap); ok {
			ipMap := v.(*sync.Map)
			// If this is a new ip
			if _, ok := ipMap.LoadOrStore(ip, uid); !ok {
				if deviceLimit > 0 {
					if deviceLimit <= aliveIp {
						ipMap.Delete(ip)
						return nil, true
					}
				}
			}
		} else if seenUID, ok := l.oldOnlineLookup(ip); ok {
			if seenUID == uid {
				l.OldUserOnline.Delete(ip)
			}
		} else {
			if deviceLimit > 0 {
				if deviceLimit <= aliveIp {
					l.UserOnlineIP.Delete(taguuid)
					return nil, true
				}
			}
		}
	}

	limit := int64(determineSpeedLimit(nodeLimit, userLimit)) * 1000000 / 8 // If you need the Speed limit
	if limit > 0 {
		if v, ok := l.SpeedLimiter.Load(taguuid); ok {
			cached := v.(*speedBucket)
			if cached.Limit == limit {
				return cached.Bucket, false
			}
		}
		Bucket = ratelimit.NewBucketWithQuantum(time.Second, limit, limit) // Byte/s
		l.SpeedLimiter.Store(taguuid, &speedBucket{Limit: limit, Bucket: Bucket})
		return Bucket, false
	} else {
		l.SpeedLimiter.Delete(taguuid)
		return nil, false
	}
}

func (l *Limiter) GetOnlineDevice() (*[]panel.OnlineUser, error) {
	onlineUser := l.Online.Snapshot()

	l.rotateDeviceWindow()

	return &onlineUser, nil
}

// rotateDeviceWindow starts a new device window: the devices of the window that
// just ended are remembered in OldUserOnline (so checkLimit does not count a
// reconnecting device as a new one) and UserOnlineIP is cleared.
//
// GetOnlineDevice calls this on every push cycle, which is what defines the
// window; checkLimit depends on it being called regularly.
func (l *Limiter) rotateDeviceWindow() {
	now := time.Now()
	l.UserOnlineIP.Range(func(key, value interface{}) bool {
		taguuid := key.(string)
		ipMap, ok := value.(*sync.Map)
		if !ok {
			return true
		}
		ipMap.Range(func(key, value interface{}) bool {
			uid, ok := value.(int)
			if !ok {
				return true
			}
			ip := netutil.NormalizeIP(key.(string))
			if ip == "" {
				return true
			}
			l.OldUserOnline.Store(ip, oldOnlineEntry{uid: uid, seenAt: now})
			return true
		})
		l.UserOnlineIP.Delete(taguuid)
		return true
	})
}

// oldOnlineLookup returns the uid last seen online from ip, if that IP was
// recorded inside the device window.
func (l *Limiter) oldOnlineLookup(ip string) (int, bool) {
	value, ok := l.OldUserOnline.Load(ip)
	if !ok {
		return 0, false
	}
	entry, ok := value.(oldOnlineEntry)
	if !ok || l.oldOnlineExpired(entry, time.Now()) {
		l.OldUserOnline.Delete(ip)
		return 0, false
	}
	return entry.uid, true
}

func (l *Limiter) oldOnlineExpired(entry oldOnlineEntry, now time.Time) bool {
	return now.Sub(entry.seenAt) > l.oldOnlineTTLValue()
}

func (l *Limiter) oldOnlineTTLValue() time.Duration {
	if l.oldOnlineTTL <= 0 {
		return defaultOldOnlineTTL
	}
	return l.oldOnlineTTL
}

// sweepOldOnline drops the IPs that were not seen online for a whole device
// window. Without it the map would grow with every client IP the node ever saw.
func (l *Limiter) sweepOldOnline() {
	now := time.Now()
	l.OldUserOnline.Range(func(key, value interface{}) bool {
		entry, ok := value.(oldOnlineEntry)
		if !ok || l.oldOnlineExpired(entry, now) {
			l.OldUserOnline.Delete(key)
		}
		return true
	})
}

func (l *Limiter) GetOnlineIPMap() (map[int][]string, error) {
	onlineUser := l.Online.Snapshot()
	data := make(map[int][]string)
	for _, onlineuser := range onlineUser {
		data[onlineuser.UID] = append(data[onlineuser.UID], onlineuser.IP)
	}
	return data, nil
}

type UserIpList struct {
	Uid    int      `json:"Uid"`
	IpList []string `json:"Ips"`
}
