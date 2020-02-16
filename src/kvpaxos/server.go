package kvpaxos

import "net"
import "fmt"
import "net/rpc"
import "log"
import "paxos"
import "sync"
import "os"
import "syscall"
import "encoding/gob"
import "math/rand"
import "time"

const (
  OpRead = 0
  OpWrite = 1
)

type OpType int

type Op struct {
  // Your definitions here.
  // Field names must start with capital letters,
  // otherwise RPC will break.
  ReqId int64
  OpType OpType
  Key string
  Value string
}

type PendingRead struct {
  seq int
  done chan string
}

type KVPaxos struct {
  mu sync.Mutex
  l net.Listener
  me int
  dead bool // for testing
  unreliable bool // for testing
  px *paxos.Paxos

  // Your definitions here.
  data map[string]string
  // the seq number of the latest applied log instance
  applied int
  // pending read requests
  pending []PendingRead
}

func (kv *KVPaxos) WaitLog(seq int) Op {
  sleepms := 10 * time.Millisecond
  for {
    decided, val := kv.px.Status(seq)
    if decided {
      op, ok := val.(Op)
      if ok {
        return op
      }
    }

    time.Sleep(sleepms)
    if sleepms < 10 * time.Second {
      sleepms *= 2
    }
  }
}

func (kv *KVPaxos) AppendOp(reqId int64, opType OpType, key string, val string) {
  for {
    maxSeq := kv.px.Max()
    nextSeq := maxSeq + 1
    op := Op{ reqId, opType, key, val }
    kv.px.Start(nextSeq, op)
    committed := kv.WaitLog(nextSeq)
    if op.ReqId == committed.ReqId {
      break
    }
  }
}

func (kv *KVPaxos) StartBackgroundWorker() {
  go func() {

  }()
}

func (kv *KVPaxos) Get(args *GetArgs, reply *GetReply) error {
  kv.AppendOp(args.ReqId, OpRead, args.Key, "")

  return nil
}


func (kv *KVPaxos) Put(args *PutArgs, reply *PutReply) error {
  kv.AppendOp(args.ReqId, OpWrite, args.Key, args.Value)
  reply.Err = OK
  return nil
}

// tell the server to shut itself down.
// please do not change this function.
func (kv *KVPaxos) kill() {
  kv.dead = true
  kv.l.Close()
  kv.px.Kill()
}

//
// servers[] contains the ports of the set of
// servers that will cooperate via Paxos to
// form the fault-tolerant key/value service.
// me is the index of the current server in servers[].
// 
func StartServer(servers []string, me int) *KVPaxos {
  // this call is all that's needed to persuade
  // Go's RPC library to marshall/unmarshall
  // struct Op.
  gob.Register(Op{})

  kv := new(KVPaxos)
  kv.me = me

  // Your initialization code here.
  kv.data = make(map[string]string)
  kv.applied = -1

  rpcs := rpc.NewServer()
  rpcs.Register(kv)

  kv.px = paxos.Make(servers, me, rpcs)

  os.Remove(servers[me])
  l, e := net.Listen("unix", servers[me]);
  if e != nil {
    log.Fatal("listen error: ", e);
  }
  kv.l = l

  // please do not change any of the following code,
  // or do anything to subvert it.

  go func() {
    for kv.dead == false {
      conn, err := kv.l.Accept()
      if err == nil && kv.dead == false {
        if kv.unreliable && (rand.Int63() % 1000) < 100 {
          // discard the request.
          conn.Close()
        } else if kv.unreliable && (rand.Int63() % 1000) < 200 {
          // process the request but force discard of reply.
          c1 := conn.(*net.UnixConn)
          f, _ := c1.File()
          err := syscall.Shutdown(int(f.Fd()), syscall.SHUT_WR)
          if err != nil {
            fmt.Printf("shutdown: %v\n", err)
          }
          go rpcs.ServeConn(conn)
        } else {
          go rpcs.ServeConn(conn)
        }
      } else if err == nil {
        conn.Close()
      }
      if err != nil && kv.dead == false {
        fmt.Printf("KVPaxos(%v) accept: %v\n", me, err.Error())
        kv.kill()
      }
    }
  }()

  return kv
}

