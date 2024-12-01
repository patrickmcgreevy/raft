package main

import (
	"fmt"
	"raft/raft"
)

const numServers = 5
func main() {
    servers := make([]raft.Server, numServers)
    for i := range servers {
        fmt.Println(servers[i])
    }
}
