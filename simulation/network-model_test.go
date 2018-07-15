package simulation

import (
	"testing"
	"time"

	"github.com/stretchr/testify/suite"
)

type NetworkModelsTestSuite struct {
	suite.Suite
}

func (n *NetworkModelsTestSuite) SetupTest() {
}

func (n *NetworkModelsTestSuite) TearDownTest() {
}

// TestNormalNetwork make sure the Delay() or NormalNetwork does not
// exceeds 200ms.
func (n *NetworkModelsTestSuite) TestNormalNetwork() {
	m := NormalNetwork{}
	for i := 0; i < 1000; i++ {
		n.Require().True(m.Delay() < 200*time.Millisecond)
	}
}

func TestNetworkModels(t *testing.T) {
	suite.Run(t, new(NetworkModelsTestSuite))
}
