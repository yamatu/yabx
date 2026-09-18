package limiter

import log "github.com/sirupsen/logrus"

// ClearOnlineIP sweeps the online state of every node.
//
// The sweep runs once a minute for the whole process, so it must not hold
// limitLock while it works: Go's RWMutex gives priority to a waiting writer, and
// a node reload (AddLimiter takes the write lock) that arrives during the sweep
// would block every GetLimiter of every node, i.e. every new connection of every
// node, until the whole sweep of all nodes finished. The list is snapshotted
// under the read lock instead and swept without it.
//
// A limiter that is deleted while it is being swept is only reachable by this
// loop, which is harmless: the sweep only lowers reference counts of an object
// that is about to be collected.
func ClearOnlineIP() error {
	log.WithField("Type", "Limiter").
		Debug("Clear online ip...")
	limitLock.RLock()
	limiters := make([]*Limiter, 0, len(limiter))
	for _, l := range limiter {
		limiters = append(limiters, l)
	}
	limitLock.RUnlock()

	for _, l := range limiters {
		l.ConnLimiter.ClearOnlineIP()
		l.Online.Sweep()
		l.sweepOldOnline()
	}
	log.WithField("Type", "Limiter").
		Debug("Clear online ip done")
	return nil
}
