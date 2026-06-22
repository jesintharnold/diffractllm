package core

type LBkind uint8

const (
	LBDirect LBkind = iota
	LBRoundRobin
	LBLeastConnection
	LBWeight
)

const (
	LB_DIRECT      = "direct"
	LB_ROUND_ROBIN = "round_robin"
	LB_LEAST_CONN  = "least_connection"
)

func (LB LBkind) String() string {
	switch LB {
	case LBDirect:
		return LB_DIRECT
	case LBRoundRobin:
		return LB_ROUND_ROBIN
	case LBLeastConnection:
		return LB_LEAST_CONN
	default:
		return ""
	}
}

func ParseLBKind(LB string) LBkind {
	switch LB {
	case LB_ROUND_ROBIN:
		return LBRoundRobin
	case LB_LEAST_CONN:
		return LBLeastConnection
	default:
		return LBDirect
	}
}
