package pbservice

import "viewservice"
import "net"
import "fmt"
import "net/rpc"
import "log"
import "time"
import "sync"
import "os"
import "syscall"
import "math/rand"


type PBServer struct {
  mu sync.Mutex
  l net.Listener
  dead bool // for testing
  unreliable bool // for testing
  me string
  vs *viewservice.Clerk
  // Your declarations here.
  lastGetViewTime time.Time
  staleView bool
  values map[string]string
  view viewservice.View
}

func (pb *PBServer) Get(args *GetArgs, reply *GetReply) error {
  // Q: does primary need to forward Get()s to backup?
  //    after all, Get() doesn't change anything, so why does backup need to know?
  //    and the extra RPC costs time
  // Q: how could we make primary-only Get()s work?
  // A: if the server is disconnected from view service, stop all get/put ops
  //    this is to avoid the split brain problem

  if pb.staleView {
    reply.Err = ErrNetworkFailure
    fmt.Printf("warning: stale view, lastGetViewTime: %s\n", pb.lastGetViewTime)
    return nil
  }

  pb.mu.Lock()
  defer pb.mu.Unlock()

  if pb.view.Primary != pb.me {
    reply.Err = ErrWrongServer
    return nil
  }

  v, ok := pb.values[args.Key]
  if !ok {
    reply.Err = ErrNoKey
  } else {
    reply.Err = OK
    reply.Value = v
  }

  return nil
}

func (pb *PBServer) Put(args *PutArgs, reply *PutReply) error {
  if pb.staleView {
    reply.Err = ErrNetworkFailure
    fmt.Printf("warning: stale view, lastGetViewTime: %s\n", pb.lastGetViewTime)
    return nil
  }

  pb.mu.Lock()
  defer pb.mu.Unlock()

  if pb.view.Primary != pb.me {
    reply.Err = ErrWrongServer
    return nil
  }

  reply.Err = OK
  if pb.view.Backup != "" {
    forward := ForwardPutArgs{args.Key, args.Value}
    var backup_reply ForwardPutReply
    ok := call(pb.view.Backup, "PBServer.ForwardPut", &forward, &backup_reply)
    if !ok {
      fmt.Printf("Failed to call PBServer.ForwardPut on backup '%s': (%s, %s)\n",
        pb.view.Backup, forward.Key, forward.Value)
      reply.Err = ErrNetworkFailure
    } else {
      if backup_reply.Err == ErrWrongServer {
        fmt.Printf("Forward to the wrong server: '%s'\n", pb.view.Backup)
        reply.Err = ErrForwardBackup
      }
    }
  }

  if reply.Err == OK {
    pb.values[args.Key] = args.Value
  }

  return nil
}

func (pb *PBServer) SendSnapshot(args *SnapshotArgs, reply *SnapshotReply) error {
  pb.mu.Lock()
  defer pb.mu.Unlock()

  if pb.view.Backup != pb.me {
    fmt.Printf("SendSnapshot: I'm not backup server: '%s'\n", pb.me)
    reply.Err = ErrWrongServer
    return nil
  }

  pb.values = args.Values
  reply.Err = OK
  return nil
}

func (pb *PBServer) ForwardPut(args *ForwardPutArgs, reply *ForwardPutReply) error {
  pb.mu.Lock()
  defer pb.mu.Unlock()

  if pb.view.Backup != pb.me {
    fmt.Printf("ForwardPut: I'm not backup server: '%s'\n", pb.me)
    reply.Err = ErrWrongServer
    return nil
  }

  pb.values[args.Key] = args.Value
  reply.Err = OK
  return nil
}

//
// ping the viewserver periodically.
// if view changed:
//   transition to new view.
//   manage transfer of state from primary to new backup.
//
func (pb *PBServer) tick() {
  view, ok := pb.vs.Get()
  now := time.Now()
  if !ok {
    if now.Sub(pb.lastGetViewTime) > viewservice.PingInterval * viewservice.DeadPings / 2 {
      pb.staleView = true
    }
    return
  } else {
    pb.lastGetViewTime = now
    pb.staleView = false
  }

  // fmt.Printf("viewnum: %d, me: %s, p: %s, b: %s\n", view.Viewnum, pb.me, view.Primary, view.Backup)
  pb.mu.Lock()
  defer pb.mu.Unlock()
  update_view := true
  if view.Primary == pb.me {
    if view.Backup == "" {
      update_view = true
    } else if view.Backup != pb.view.Backup {
      snapshot := SnapshotArgs{pb.values}
      var reply SnapshotReply
      ok := call(view.Backup, "PBServer.SendSnapshot", &snapshot, &reply)
      if !ok {
        fmt.Printf("Failed to call PBServer.SendSnapshot on backup: '%s'\n", view.Backup)
        update_view = false
      } else if reply.Err == OK {
        fmt.Printf("Successfully send snapshot to backup: '%s'\n", view.Backup)
        update_view = true
      } else {
        fmt.Printf("Failed to send snapshot to backup '%s': %s\n", view.Backup, reply.Err)
        update_view = false
      }
    }
  }
  // if view.Backup == pb.Me {
  //   if pb.view.Backup != pb.Me {
  //     
  //   }
  // }
  if update_view {
    pb.view = view
  }
  pb.vs.Ping(pb.view.Viewnum)
}

// tell the server to shut itself down.
// please do not change this function.
func (pb *PBServer) kill() {
  pb.dead = true
  pb.l.Close()
}


func StartServer(vshost string, me string) *PBServer {
  pb := new(PBServer)
  pb.me = me
  pb.vs = viewservice.MakeClerk(me, vshost)
  // Your pb.* initializations here.
  pb.staleView = true
  pb.lastGetViewTime = time.Now()
  pb.values = make(map[string]string)

  rpcs := rpc.NewServer()
  rpcs.Register(pb)

  os.Remove(pb.me)
  l, e := net.Listen("unix", pb.me);
  if e != nil {
    log.Fatal("listen error: ", e);
  }
  pb.l = l

  // please do not change any of the following code,
  // or do anything to subvert it.

  go func() {
    for pb.dead == false {
      conn, err := pb.l.Accept()
      if err == nil && pb.dead == false {
        if pb.unreliable && (rand.Int63() % 1000) < 100 {
          // discard the request.
          conn.Close()
        } else if pb.unreliable && (rand.Int63() % 1000) < 200 {
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
      if err != nil && pb.dead == false {
        fmt.Printf("PBServer(%v) accept: %v\n", me, err.Error())
        pb.kill()
      }
    }
  }()

  go func() {
    for pb.dead == false {
      pb.tick()
      time.Sleep(viewservice.PingInterval)
    }
  }()

  return pb
}
