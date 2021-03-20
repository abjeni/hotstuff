package chainedhotstuff

import (
	"time"

	"github.com/relab/hotstuff"
	"github.com/relab/hotstuff/blockchain"
	"github.com/relab/hotstuff/config"
	"github.com/relab/hotstuff/crypto/ecdsa"
	"github.com/relab/hotstuff/leaderrotation"
	"github.com/relab/hotstuff/synchronizer"
)

func DefaultModules(replicaConfig config.ReplicaConfig, baseTimeout time.Duration) hotstuff.Builder {
	builder := hotstuff.NewBuilder(replicaConfig.ID, replicaConfig.PrivateKey)
	signer, verifier := ecdsa.NewWithCache(2 * len(replicaConfig.Replicas))
	builder.Register(
		New(),
		synchronizer.New(baseTimeout),
		leaderrotation.NewRoundRobin(),
		blockchain.New(100),
		signer,
		verifier,
	)
	return builder
}