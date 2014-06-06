package pbservice

const (
  OK = "OK"
  ErrNoKey = "ErrNoKey"
  ErrWrongServer = "ErrWrongServer"
  ErrForwardBackup = "ErrForwardBackup"
)
type Err string

type PutArgs struct {
  Key string
  Value string
}

type PutReply struct {
  Err Err
}

type GetArgs struct {
  Key string
}

type GetReply struct {
  Err Err
  Value string
}

// Your RPC definitions here.
type SnapshotArgs struct {
  Values map[string]string
}

type SnapshotReply struct {
  Err Err
}

type ForwardPutArgs struct {
  Key string
  Value string
}

type ForwardPutReply struct {
  Err Err
}
