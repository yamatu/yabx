package node

import (
	"fmt"

	"github.com/InazumaV/V2bX/api/panel"
	"github.com/InazumaV/V2bX/conf"
	vCore "github.com/InazumaV/V2bX/core"
	log "github.com/sirupsen/logrus"
)

type Node struct {
	controllers []*Controller
}

func New() *Node {
	return &Node{}
}

// Start creates and starts one controller per configured node.
//
// Controllers that were already started are closed when a later node fails to
// start, so a broken configuration cannot leak goroutines, timers or limiter
// entries, and Node.Close never has to walk half initialized controllers.
func (n *Node) Start(nodes []conf.NodeConfig, core vCore.Core) error {
	controllers := make([]*Controller, 0, len(nodes))
	for i := range nodes {
		p, err := panel.New(&nodes[i].ApiConfig)
		if err != nil {
			n.closeControllers(controllers)
			return err
		}
		// Register controller service
		c := NewController(core, p, &nodes[i].Options)
		if err = c.Start(); err != nil {
			n.closeControllers(append(controllers, c))
			return fmt.Errorf("start node controller [%s-%s-%d] error: %s",
				nodes[i].ApiConfig.APIHost,
				nodes[i].ApiConfig.NodeType,
				nodes[i].ApiConfig.NodeID,
				err)
		}
		controllers = append(controllers, c)
	}
	n.controllers = controllers
	return nil
}

func (n *Node) Close() {
	n.closeControllers(n.controllers)
	n.controllers = nil
}

// closeControllers stops the given controllers.
//
// Closing must never take the whole process down: an already removed node or a
// node that never finished starting reports an error here.
func (n *Node) closeControllers(controllers []*Controller) {
	for _, c := range controllers {
		if c == nil {
			continue
		}
		if err := c.Close(); err != nil {
			log.WithField("tag", c.getTag()).Errorf("Close node controller failed: %s", err)
		}
	}
}
