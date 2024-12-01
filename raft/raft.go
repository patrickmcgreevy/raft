package raft

import "fmt"

type Server struct {
    // We will use net/rpc to make this work

	// Persistent state. This should be written to stable storage before
	// responding to RPCs.
	state    leadership // Leadership state of the server.
	cur      term       // Monotonically increases when a new leader is elected.
	votedFor int        // Updated when the server votes for a candidate.
	log      log        // Each entry contains command for state machine, and term when entry was received by leader.

	// Volatile state.
	commitIndex index // Highest log entry known to be committed. Increases monotonically.
	lastApplied index // Highest log entry applied to state machine. Increases monotonically.

	// Volatile state on leaders. It should be reinitialized after election.
	nextIndex  []index // For each server, the next log entry to send to that server. Initialized to leader last log inex +1
	matchIndex []index // For each server, highest log entry known to be replicated on that server. Increases monotonically.
}

func (s *Server) String() string {
	return fmt.Sprintf("server is a %s. It is %s. It voted for %d.", s.state, s.cur, s.votedFor)
}

func (s *Server) ListenAndServe() {
}

// Contains a command for state machine, and term when entry was received by leader.
type entry struct {
	received term // When entry was received by leader.
	c        cmd  // Command for state machine.
}
type log []entry

// This is a TBD. I'm still not sure what the state-machine here will look like.
type cmd struct{}

type leadership int

const (
	candidate leadership = iota
	leader
	follower
)

func (l leadership) String() string {
	switch l {
	case leader:
		return "leader"
	case follower:
		return "follower"
	case candidate:
		return "candidate"
	default:
		panic(fmt.Errorf("unknown leadership: %d", l))
	}
}

type term int

func (t term) String() string {
	return fmt.Sprintf("raft term: %d", t)
}

type index int
