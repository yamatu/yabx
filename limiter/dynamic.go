package limiter

import (
	"time"

	"github.com/InazumaV/V2bX/api/panel"
	"github.com/InazumaV/V2bX/common/format"
)

// AddDynamicSpeedLimit sets a temporary speed limit for a user.
//
// An entry that already exists is updated through a copy: replacing it would
// drop the uid (the online device would then be reported as uid 0) and the
// device limit.
func (l *Limiter) AddDynamicSpeedLimit(tag string, userInfo *panel.UserInfo, limitNum int, expire int64) error {
	taguuid := format.UserTag(tag, userInfo.Uuid)
	expireTime := time.Now().Add(time.Duration(expire) * time.Second).Unix()
	if err := l.updateUserLimit(taguuid, func(u *UserLimitInfo) {
		u.DynamicSpeedLimit = limitNum
		u.ExpireTime = expireTime
	}); err != nil {
		l.UserLimitInfo.Store(taguuid, &UserLimitInfo{
			UID:               userInfo.Id,
			DynamicSpeedLimit: limitNum,
			ExpireTime:        expireTime,
		})
	}
	l.SpeedLimiter.Delete(taguuid)
	return nil
}

// determineSpeedLimit returns the minimum non-zero rate
func determineSpeedLimit(limit1, limit2 int) (limit int) {
	if limit1 == 0 || limit2 == 0 {
		if limit1 > limit2 {
			return limit1
		} else if limit1 < limit2 {
			return limit2
		} else {
			return 0
		}
	} else {
		if limit1 > limit2 {
			return limit2
		} else if limit1 < limit2 {
			return limit1
		} else {
			return limit1
		}
	}
}
