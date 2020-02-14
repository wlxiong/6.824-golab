package paxos

const (
	OK = "OK"
	Reject = "Reject"
)

type Err string

type PrepareArgs struct {
	Seq int
	N int
	Peer int
	DoneSeq int
}

type PrepareReply struct {
	Seq int
	Who int
	Np int
	Na int
	Va interface{}
	Err Err
}

type AcceptArgs struct {
	Seq int
	N int
	V interface{}
	Peer int
	DoneSeq int
}

type AcceptReply struct {
	Seq int
	Who int
	Np int
	N int
	Err Err
}

type DecidedArgs struct {
	Seq int
	N int
	V interface {}
	Peer int
	DoneSeq int
}

type DecidedReply struct {
	Err Err
}
