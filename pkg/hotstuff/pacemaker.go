package hotstuff

// Pacemaker is a mechanism that provides synchronization
type Pacemaker interface {
	GetLeader() ReplicaID
}

// FixedLeaderPacemaker uses a fixed leader.
type FixedLeaderPacemaker struct {
	HS     *HotStuff
	Leader ReplicaID
}

// GetLeader returns the fixed ID of the leader
func (p FixedLeaderPacemaker) GetLeader() ReplicaID {
	return p.Leader
}

// Beat make the leader brodcast a new proposal for a node to work on.
func (p FixedLeaderPacemaker) Beat() {
	logger.Println("Beat")
	if p.HS.id == p.GetLeader() {
		p.HS.Propose()
	}
}

// Start runs the pacemaker which will beat when the previous QC is completed
func (p FixedLeaderPacemaker) Start() {
	notify := p.HS.GetNotifier()
	if p.HS.id == p.Leader {
		go p.Beat()
	}
	for n := range notify {
		switch n.Event {
		case QCFinish:
			if p.HS.id == p.Leader {
				go p.Beat()
			}
		}
	}
}
