package types

// MembershipActionType specifies the action of a membership event.
type MembershipActionType int

// Event enums.
const (
	MembershipActionAdd MembershipActionType = iota
	MembershipActionDelete
)

// MembershipEvent specifies the event of membership changes.
type MembershipEvent struct {
	Epoch  int
	Action MembershipActionType
}
