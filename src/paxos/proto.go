package paxos

const (
	OK = "OK"
	Reject = "Reject"
)

type Err string

type PrepareArgs struct {
	Seq int
	N int
}

type PrepareReply struct {
	Seq int
	Na int
	Va interface{}
	Err Err
}

type AcceptArgs struct {
	Seq int
	N int
	V interface{}
}

type AcceptReply struct {
	Seq int
	N int
	Err Err
}

type DecidedArgs struct {
	Seq int
	N int
	V interface {}
}

type DecidedReply struct {
	Err Err
}
