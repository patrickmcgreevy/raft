package raft

import (
	"fmt"
	"net"
	"net/rpc"
	"sync"
	"time"
)

// Names of fields within this struct are derived from the Raft paper. Therefore,
// go idioms are ignored for algorithmic consistency.
type Server struct {
	id int

	// Persistent state. This should be written to stable storage before
	// responding to RPCs.
	state       leadership // Leadership state of the server.
	currentTerm term       // Monotonically increases when a new leader is elected.
	votedFor    int        // Updated when the server votes for a candidate.
	log         log        // Each entry contains command for state machine, and term when entry was received by leader.

	// Volatile state.
	commitIndex index // Highest log entry known to be committed. Increases monotonically.
	lastApplied index // Highest log entry applied to state machine. Increases monotonically.

	// Volatile state on leaders. It should be reinitialized after election.
	nextIndex  []index // For each server, the next log entry to send to that server. Initialized to leader last log inex +1
	matchIndex []index // For each server, highest log entry known to be replicated on that server. Increases monotonically.

	// Not state defined by the algorithm. But it refers to an election timer.
	electionTimer <-chan time.Time

	peers []*Server // The servers peers are used to reach consensus.
}

func (s *Server) ListenAndServe() error {
	err := rpc.Register(s)
	if err != nil {
		return fmt.Errorf("cannot start raft server: %w", err)
	}
	l, err := net.Listen("unix", s.Address())
	if err != nil {
		return fmt.Errorf("cannot start raft server: %w", err)
	}
	unixListener := l.(*net.UnixListener)
	unixListener.SetUnlinkOnClose(true)
	defer unixListener.Close()
	s.resetElectionTimer()

	for {
		// All Servers:
		//  • If commitIndex > lastApplied: increment lastApplied, apply
		//      log[lastApplied] to state machine (§5.3)
		//  • If RPC request or response contains term T > currentTerm:
		//      set currentTerm = T, convert to follower (§5.1)
		if s.commitIndex > s.lastApplied {
			s.lastApplied++
		}
		// TODO: apply last log to state machine.
		// TODO: how do I do the convert to follower thing??

		switch s.state {
		case follower:
			// Followers (§5.2):
			//  • Respond to RPCs from candidates and leaders
			//  • If election timeout elapses without receiving AppendEntries
			//      RPC from current leader or granting vote to candidate:
			//      convert to candidate
			conn, err := unixListener.Accept()
			if err != nil {
				// The timeout just means that we think the leader is dead.
				// Therefore, we start a new election.
				if neterr, ok := err.(net.Error); ok && neterr.Timeout() {
					s.state = candidate
					continue
				}
				// Now this is an unhandled error. DIE! But don't panic.
				return fmt.Errorf("raft server died unexpectedly: %w", err)
			}
			rpc.ServeConn(conn)
			conn.Close()
			select {
			case <-s.electionTimer:
				// This just means that we think the leader is dead.
				// Therefore, we start a new election.
				s.state = candidate
			default:
				// If the timer hasn't fired, there's nothing to do.
			}
		case candidate:
			// Candidates (§5.2):
			//     • On conversion to candidate, start election:
			//     • Increment currentTerm
			s.currentTerm++
			//     • Vote for self
			s.votedFor = s.id
			//     • Reset election timer
			s.resetElectionTimer()
			//     • Send RequestVote RPCs to all other servers
			var wg sync.WaitGroup
			votes := 0
			for _, peer := range s.peers {
				wg.Add(1)
				reply := RequestVoteReply{}
				go func(peer *Server) {
					defer wg.Done()
					client, err := rpc.Dial("unix", peer.Address())
					if err != nil {
						fmt.Println(err)
						return
					}
					var t term
					if len(s.log) != 0 {
						t = s.log[len(s.log)-1].received
					}
					err = client.Call(
						"Server.RequestVote",
						RequestVoteArgs{
							CandidateId:   s.id,
							CandidateTerm: s.currentTerm,
							LastLogIndex:  index(max(len(s.log), 0)),
							LastLogTerm:   t,
						},
						&reply,
					)
					if err != nil {
						fmt.Println(err)
						return
					}
                    if reply.Term > s.currentTerm {
                        s.currentTerm = reply.Term
                        s.state = follower
                    }
                    if reply.VoteGranted {
                        votes++
                    }
				}(peer)
			}
			//     • If votes received from majority of servers: become leader
            wg.Wait()
            // If we received a reply from the leader, we will set our state to follower.
            if s.state != candidate {
                continue
            }
			//     • If AppendEntries RPC received from new leader: convert to
			//         follower
			//     • If election timeout elapses: start new election
		case leader:
			// Leaders:
			//     • Upon election: send initial empty AppendEntries RPCs
			//         (heartbeat) to each server; repeat during idle periods to
			//         prevent election timeouts (§5.2)
			//     • If command received from client: append entry to local log,
			//         respond after entry applied to state machine (§5.3)
			//     • If last log index ≥ nextIndex for a follower: send
			//         AppendEntries RPC with log entries starting at nextIndex
			//     • If successful: update nextIndex and matchIndex for
			//         follower (§5.3)
			//     • If AppendEntries fails because of log inconsistency:
			//         decrement nextIndex and retry (§5.3)
			//     • If there exists an N such that N > commitIndex, a majority
			//         of matchIndex[i] ≥ N, and log[N].term == currentTerm:
			//         set commitIndex = N (§5.3, §5.4).
		}
	}

	return nil
}

func (s *Server) resetElectionTimer() {
	s.electionTimer = time.NewTimer(electionTimeout).C
}

func (s *Server) AppendEntries(args AppendEntriesArgs, reply *AppendEntriesReply) (err error) {
	defer func() {
		if err != nil {
			reply.Success = false
			err = fmt.Errorf("AppendEntries RPC failed: %w", err)
		}
	}()
    if args.Cur > s.currentTerm {
        s.currentTerm = args.Cur
        s.state = follower
    }
	if args.Cur < s.currentTerm {
		return fmt.Errorf("caller's term (%d) is less than the server's term (%d)", args.Cur, s.currentTerm)
	}

	if int(args.PrevLogIndex) >= len(s.log) {
		return fmt.Errorf("the server does not have an entry at index %d", args.PrevLogIndex)
	}

	if s.log[args.PrevLogIndex].received != args.PrevLogTerm {
		return fmt.Errorf(
			"the server's log entry at index %d does not have term %d: actual %s",
			args.PrevLogIndex,
			args.PrevLogTerm,
			s.log[args.PrevLogIndex],
		)
	}

	for i := 0; i < len(args.Entries); i++ {
		logIndex := int(args.PrevLogIndex) + i
		if logIndex >= len(s.log) {
			s.log = append(s.log, args.Entries[i])
			continue
		}
		if s.log[logIndex].received != args.Entries[i].received {
			s.log = s.log[:logIndex]
			s.log = append(s.log, args.Entries[i])
		}
	}

	if args.LeaderCommit > s.commitIndex {
		s.commitIndex = min(args.LeaderCommit, index(len(args.Entries)-1))
	}

	// If I get this RPC, is it guaranteed to be from the leader?
	s.resetElectionTimer()

	return nil
}

func (s *Server) RequestVote(args RequestVoteArgs, reply *RequestVoteReply) error {
    if args.CandidateTerm > s.currentTerm {
        s.currentTerm = args.CandidateTerm
        s.state = follower
    }
	reply.Term = s.currentTerm
	if args.CandidateTerm < s.currentTerm {
		reply.VoteGranted = false
		return fmt.Errorf("candidate %d's log is behind server %d's", args.CandidateId, s.id)
	}

	if s.votedFor == 0 || s.votedFor == args.CandidateId {
		reply.VoteGranted = true
	}

	// Should I only reset the timer if s is a follower?
	// The algorithm requires us to reset the electionTimer when we grant a vote.
	if reply.VoteGranted == true {
		s.resetElectionTimer()
	}

	return nil
}

func (s *Server) Address() string {
	return fmt.Sprintf("/tmp/raft/run/raft.%d", s.id)
}

func (s Server) String() string {
	return fmt.Sprintf("server is a %s. It is %s. It voted for %d.", s.state, s.currentTerm, s.votedFor)
}

// For usage with net/rpc package.
// Names of fields within this struct are derived from the Raft paper. Therefore,
// go idioms are ignored for algorithmic consistency.
type AppendEntriesArgs struct {
	Cur          term  // Leader's term.
	LeaderId     int   // Leader ID.
	PrevLogIndex index // Index of log entry immediately preceeding new ones.
	PrevLogTerm  term  // Term of log entry immediately preceeding new ones.
	Entries      log   // May be empty for heartbeat.
	LeaderCommit index // Leader's commit index.
}

// For usage with net/rpc package.
// Names of fields within this struct are derived from the Raft paper. Therefore,
// go idioms are ignored for algorithmic consistency.
type AppendEntriesReply struct {
	Term    term // For the leader to update himself.
	Success bool
}

// For usage with net/rpc package.
// Names of fields within this struct are derived from the Raft paper. Therefore,
// go idioms are ignored for algorithmic consistency.
type RequestVoteArgs struct {
	CandidateId   int   // Leader's id.
	CandidateTerm term  // The requesting candidates term.
	LastLogIndex  index // Index of candidate's last log entry.
	LastLogTerm   term  // Term of candidate's last log entry.
}

// For usage with net/rpc package.
// Names of fields within this struct are derived from the Raft paper. Therefore,
// go idioms are ignored for algorithmic consistency.
type RequestVoteReply struct {
	Term        term // For candidate to update itself.
	VoteGranted bool // True indicates that candidate received vote.
}

// Contains a command for state machine, and term when entry was received by leader.
type entry struct {
	received term // When entry was received by leader.
	c        cmd  // Command for state machine.
}

func (e entry) String() string {
	return fmt.Sprintf("received: %d command: %s", e.received, e.c)
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

const electionTimeout time.Duration = 30 * time.Second

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
