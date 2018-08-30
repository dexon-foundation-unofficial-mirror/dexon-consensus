package core

import (
	"testing"

	"github.com/stretchr/testify/suite"
)

type UtilsTestSuite struct {
	suite.Suite
}

func (s *UtilsTestSuite) TestRemoveFromSortedIntSlice() {
	// Remove something exists.
	xs := []int{1, 2, 3, 4, 5}
	s.Equal(
		removeFromSortedIntSlice(xs, 3),
		[]int{1, 2, 4, 5})
	// Remove something not exists.
	s.Equal(removeFromSortedIntSlice(xs, 6), xs)
	// Remove from empty slice, should not panic.
	s.Equal([]int{}, removeFromSortedIntSlice([]int{}, 1))
}

func TestUtils(t *testing.T) {
	suite.Run(t, new(UtilsTestSuite))
}
