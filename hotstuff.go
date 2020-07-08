package hotstuff

import (
	"context"
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	"github.com/relab/hotstuff/config"
	"github.com/relab/hotstuff/consensus"
	"github.com/relab/hotstuff/internal/logging"
	"github.com/relab/hotstuff/internal/proto"
	"google.golang.org/grpc"
)

var logger *log.Logger

func init() {
	logger = logging.GetLogger()
}

// Pacemaker is a mechanism that provides synchronization
type Pacemaker interface {
	GetLeader(view int) config.ReplicaID
	Init(*HotStuff)
}

// HotStuff is a thing
type HotStuff struct {
	*consensus.HotStuffCore

	pacemaker Pacemaker

	nodes map[config.ReplicaID]*proto.Node

	server  *hotstuffServer
	manager *proto.Manager
	cfg     *proto.Configuration

	closeOnce sync.Once

	qcTimeout      time.Duration
	connectTimeout time.Duration
}

//New creates a new GorumsHotStuff backend object.
func New(conf *config.ReplicaConfig, pacemaker Pacemaker, connectTimeout, qcTimeout time.Duration) *HotStuff {
	hs := &HotStuff{
		pacemaker:      pacemaker,
		HotStuffCore:   consensus.New(conf),
		nodes:          make(map[config.ReplicaID]*proto.Node),
		connectTimeout: connectTimeout,
		qcTimeout:      qcTimeout,
	}
	pacemaker.Init(hs)
	return hs
}

//Start starts the server and client
func (hs *HotStuff) Start() error {
	addr := hs.Config.Replicas[hs.Config.ID].Address
	err := hs.startServer(addr)
	if err != nil {
		return fmt.Errorf("Failed to start GRPC Server: %w", err)
	}
	err = hs.startClient(hs.connectTimeout)
	if err != nil {
		return fmt.Errorf("Failed to start GRPC Clients: %w", err)
	}
	return nil
}

func (hs *HotStuff) startClient(connectTimeout time.Duration) error {
	idMapping := make(map[string]uint32, len(hs.Config.Replicas)-1)
	for _, replica := range hs.Config.Replicas {
		if replica.ID != hs.Config.ID {
			idMapping[replica.Address] = uint32(replica.ID)
		}
	}

	mgr, err := proto.NewManager(proto.WithGrpcDialOptions(
		grpc.WithBlock(),
		grpc.WithInsecure(),
	),
		proto.WithDialTimeout(connectTimeout),
		proto.WithNodeMap(idMapping),
	)
	if err != nil {
		return fmt.Errorf("Failed to connect to replicas: %w", err)
	}
	hs.manager = mgr

	for _, node := range mgr.Nodes() {
		hs.nodes[config.ReplicaID(node.ID())] = node
	}

	hs.cfg, err = hs.manager.NewConfiguration(hs.manager.NodeIDs(), &struct{}{})
	if err != nil {
		return fmt.Errorf("Failed to create configuration: %w", err)
	}

	return nil
}

// startServer runs a new instance of hotstuffServer
func (hs *HotStuff) startServer(port string) error {
	lis, err := net.Listen("tcp", port)
	if err != nil {
		return fmt.Errorf("Failed to listen to port %s: %w", port, err)
	}

	hs.server = &hotstuffServer{hs, proto.NewGorumsServer()}
	hs.server.RegisterHotstuffServer(hs.server)

	go hs.server.Serve(lis)
	return nil
}

// Close closes all connections made by the HotStuff instance
func (hs *HotStuff) Close() {
	hs.closeOnce.Do(func() {
		hs.HotStuffCore.Close()
		hs.manager.Close()
		hs.server.Stop()
	})
}

// Propose broadcasts a new proposal to all replicas
func (hs *HotStuff) Propose() {
	proposal := hs.CreateProposal()
	logger.Printf("Propose (%d commands): %s\n", len(proposal.Commands), proposal)
	protobuf := proto.BlockToProto(proposal)
	hs.cfg.Propose(protobuf)
	// self-vote
	hs.server.Propose(nil, protobuf)
}

// SendNewView sends a NEW-VIEW message to a specific replica
func (hs *HotStuff) SendNewView(id config.ReplicaID) {
	qc := hs.GetQCHigh()
	if node, ok := hs.nodes[id]; ok {
		node.NewView(proto.QuorumCertToProto(qc))
	}
}

type hotstuffServer struct {
	*HotStuff
	*proto.GorumsServer
}

// Propose handles a replica's response to the Propose QC from the leader
func (hs *hotstuffServer) Propose(ctx context.Context, protoB *proto.Block) {
	block := protoB.FromProto()
	p, err := hs.OnReceiveProposal(block)
	if err != nil {
		logger.Println("OnReceiveProposal returned with error:", err)
		return
	}
	leaderID := hs.pacemaker.GetLeader(block.Height)
	if hs.Config.ID == leaderID {
		hs.OnReceiveVote(p)
	} else if leader, ok := hs.nodes[leaderID]; ok {
		leader.Vote(proto.PartialCertToProto(p))
	}
}

func (hs *hotstuffServer) Vote(ctx context.Context, cert *proto.PartialCert) {
	hs.OnReceiveVote(cert.FromProto())
}

// NewView handles the leader's response to receiving a NewView rpc from a replica
func (hs *hotstuffServer) NewView(ctx context.Context, msg *proto.QuorumCert) {
	qc := msg.FromProto()
	hs.OnReceiveNewView(qc)
}
