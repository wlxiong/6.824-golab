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
  OpInvalid = 0
  OpRead = 1
  OpWrite = 2
  OpCount = 3
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
  reqId int64
  seq int
  commited bool
  done chan *string
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
  pendingRead map[int64]*PendingRead
}

func (kv *KVPaxos) WaitLog(seq int, timeout time.Duration) (bool, Op) {
  start := time.Now()
  sleepms := 10 * time.Millisecond
  for !kv.dead {
    decided, val := kv.px.Status(seq)
    if decided {
      op, ok := val.(Op)
      if ok {
        return true, op
      }
    }

    if timeout > 0 {
      now := time.Now()
      elasped := now.Sub(start)
      if elasped.Milliseconds() >= timeout.Milliseconds() {
        return false, Op{ -1, OpCount, "", "" }
      }

      remaining := timeout - elasped
      if sleepms > remaining {
        sleepms = remaining
      }
    }

    time.Sleep(sleepms)
    if sleepms < time.Second {
      sleepms *= 2
    }
  }

  return false, Op{ -1, OpCount, "", "" }
}

func (kv *KVPaxos) AssignNewSeqToNewRequest(reqId int64, opType OpType) int {
  kv.mu.Lock()
  defer kv.mu.Unlock()
  maxSeq := kv.px.Max()
  nextSeq := maxSeq + 1
  if opType == OpRead {
    pendingRead, ok := kv.pendingRead[reqId]
    if ok {
      pendingRead.seq = nextSeq
    } else {
      kv.pendingRead[reqId] = &PendingRead{ reqId, nextSeq, false, make(chan *string) }
    }
  }
  return nextSeq
}

func (kv *KVPaxos) MarkPendingReadCommitted(reqId int64) *PendingRead {
  kv.mu.Lock()
  defer kv.mu.Unlock()
  pendingRead, ok := kv.pendingRead[reqId]
  if ok {
    pendingRead.commited = true
  }
  return pendingRead
}

func (kv *KVPaxos) GetPendingRead(reqId int64) *PendingRead {
  kv.mu.Lock()
  defer kv.mu.Unlock()
  pendingRead := kv.pendingRead[reqId]
  return pendingRead
}

func (kv *KVPaxos) RemovePendingRead(reqId int64) *PendingRead {
  kv.mu.Lock()
  defer kv.mu.Unlock()
  pendingRead := kv.pendingRead[reqId]
  delete(kv.pendingRead, reqId)
  return pendingRead
}

func (kv *KVPaxos) GetMinSeqOfUncommittedRead() int {
  kv.mu.Lock()
  defer kv.mu.Unlock()
  minSeq := kv.px.Max() + 1
  for _, pendingRead := range kv.pendingRead {
    if pendingRead.commited { continue }
    if minSeq < 0 || minSeq > pendingRead.seq {
      minSeq = pendingRead.seq
    }
  }
  return minSeq
}

func (kv *KVPaxos) AppendOp(reqId int64, opType OpType, key string, val string) int {
  op := Op{ reqId, opType, key, val }
  for !kv.dead {
    seq := kv.AssignNewSeqToNewRequest(reqId, opType)
    kv.px.Start(seq, op)
    ok, committed := kv.WaitLog(seq, 0)
    if ok && op.ReqId == committed.ReqId {
      return seq
    }
  }
  return -1
}

func (kv *KVPaxos) StartBackgroundWorker() {
  go func() {
    log.Printf("[kv][%d] background worker started", kv.me)
    writeSeen := make(map[int64]bool)
    for !kv.dead {
      uncommitted := kv.GetMinSeqOfUncommittedRead()
      log.Printf("[%d] uncommitted seq: %d", kv.me, uncommitted)
      for seq := kv.applied + 1; seq <= uncommitted; seq++ {
        ok, op := kv.WaitLog(seq, 5 * time.Second)
        log.Printf("[%d] operation %+v", kv.me, op)
        if ok {
          if op.OpType == OpRead {
            pendingRead := kv.GetPendingRead(op.ReqId)
            if pendingRead == nil { continue }
            log.Printf("[kv][%d] pending read %+v", kv.me, pendingRead)
            val, ok := kv.data[op.Key]
            log.Printf("[kv][%d] read key %s val %s", kv.me, op.Key, val)
            if ok {
              pendingRead.done <- &val
            } else {
              pendingRead.done <- nil
            }
          } else if op.OpType == OpWrite {
            if writeSeen[op.ReqId] {
              log.Printf("[kv][%d] duplicate write key %s val %s", kv.me, op.Key, op.Value)
            } else {
              kv.data[op.Key] = op.Value
              writeSeen[op.ReqId] = true
              log.Printf("[kv][%d] write key %s val %s", kv.me, op.Key, op.Value)
            }
          } else {
            log.Printf("[kv][%d] committed invalid op %+v seq %d", kv.me, op, seq)
          }
        } else {
          kv.px.Start(seq, nil)
        }
      }
      kv.applied = uncommitted
    }
  }()
}

func (kv *KVPaxos) Get(args *GetArgs, reply *GetReply) error {
  log.Printf("[kv][%d] GET request %+v", kv.me, args)
  seq := kv.AppendOp(args.ReqId, OpRead, args.Key, "")
  log.Printf("[kv][%d] GET committed, seq %d, key %s", kv.me, seq, args.Key)
  pendingRead := kv.MarkPendingReadCommitted(args.ReqId)
  val := <- pendingRead.done
  kv.RemovePendingRead(args.ReqId)
  if val != nil {
    reply.Value = *val
    reply.Err = OK
  } else {
    reply.Err = ErrNoKey
  }
  return nil
}

func (kv *KVPaxos) Put(args *PutArgs, reply *PutReply) error {
  log.Printf("[kv][%d] PUT request %+v", kv.me, args)
  seq := kv.AppendOp(args.ReqId, OpWrite, args.Key, args.Value)
  log.Printf("[kv][%d] PUT committed, seq %d, key %s, val %s", kv.me, seq, args.Key, args.Value)
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
  kv.pendingRead = make(map[int64]*PendingRead)
  kv.applied = -1

  rpcs := rpc.NewServer()
  rpcs.Register(kv)

  kv.px = paxos.Make(servers, me, rpcs)

  // start worker
  kv.StartBackgroundWorker()

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

