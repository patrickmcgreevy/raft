package main

import (
	"fmt"
	"net/rpc"
	"raft/raft"
	"time"
)

const numServers = 5
func main() {
    servers := make([]raft.Server, numServers)
    for i := range servers {
        fmt.Println(servers[i])
    }
    go servers[0].ListenAndServe()
    time.Sleep(3*time.Second)
    client, err := rpc.Dial("unix", servers[0].Address())
    if err != nil {
        fmt.Println(err)
        return
    }
    reply := raft.RequestVoteReply{}
    err = client.Call(
        "Server.RequestVote",
        raft.RequestVoteArgs{CandidateId: 1, CandidateTerm: 1, LastLogIndex: -1, LastLogTerm: -1},
        &reply,
    )
    if err != nil {
        fmt.Println(err)
    }
    fmt.Println(reply)
}
