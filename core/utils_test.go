package core

import (
	"testing"

	"github.com/stretchr/testify/suite"
)

type UtilsTestSuite struct {
	suite.Suite
}

func (s *UtilsTestSuite) TestRemoveFromSortedUint32Slice() {
	// Remove something exists.
	xs := []uint32{1, 2, 3, 4, 5}
	s.Equal(
		removeFromSortedUint32Slice(xs, 3),
		[]uint32{1, 2, 4, 5})
	// Remove something not exists.
	s.Equal(removeFromSortedUint32Slice(xs, 6), xs)
	// Remove from empty slice, should not panic.
	s.Equal([]uint32{}, removeFromSortedUint32Slice([]uint32{}, 1))
}

func TestUtils(t *testing.T) {
	suite.Run(t, new(UtilsTestSuite))
}
