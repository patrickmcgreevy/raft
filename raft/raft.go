package raft

import "fmt"

type Server struct {
    state serverState
}

func (s Server) String() string {
    return fmt.Sprintf("server is in state %s", s.state)
}

type serverState int
const(
    candidate serverState = iota
    leader
    follower
)

func (ss serverState) String() string {
    switch ss {
    case leader:
        return "leader"
    case follower:
        return "follower"
    case candidate:
        return "candidate"
    default:
        panic(fmt.Errorf("unknown serverState: %d", ss))
    }
}
