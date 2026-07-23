package core

import (
	"encoding/json"
	"fmt"
)

type LBKind uint8

func (m LBKind) MarshalJSON() ([]byte, error) { return json.Marshal(m.String()) }

func (m *LBKind) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	v, err := ParseLBKind(s)
	if err != nil {
		return err
	}
	*m = v
	return nil
}

const (
	LBRoundRobin LBKind = iota
	LBLeastConnection
	LBLatencyBased
)

const (
	LBRoundRobinName      = "round_robin"
	LBLeastConnectionName = "least_connection"
	LBLatencyBasedName    = "latency_based"
)

func (LB LBKind) String() string {
	switch LB {
	case LBRoundRobin:
		return LBRoundRobinName
	case LBLeastConnection:
		return LBLeastConnectionName
	case LBLatencyBased:
		return LBLatencyBasedName
	default:
		return ""
	}
}

func ParseLBKind(value string) (LBKind, error) {
	switch value {
	case LBRoundRobinName:
		return LBRoundRobin, nil
	case LBLeastConnectionName:
		return LBLeastConnection, nil
	case LBLatencyBasedName:
		return LBLatencyBased, nil
	default:
		return 0, fmt.Errorf("unknown load balancer algorithm %q", value)
	}
}
