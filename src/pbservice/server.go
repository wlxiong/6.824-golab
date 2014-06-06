package pbservice

import "github.com/wlxiong/6.824-golab/viewservice"
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
  values map[string]string
  view viewservice.View
}

func (pb *PBServer) Get(args *GetArgs, reply *GetReply) error {
  if pb.view.Primary != pb.me {
    reply.Err = ErrWrongServer
    return nil
  }

  pb.mu.Lock()
  defer pb.mu.Unlock()
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
  if pb.view.Primary != pb.me {
    reply.Err = ErrWrongServer
    return nil
  }

  pb.mu.Lock()
  defer pb.mu.Unlock()
  pb.values[args.Key] = args.Value
  reply.Err = OK
  forward := ForwardPutArgs{args.Key, args.Value}
  var fw_reply ForwardPutReply
  ok := call(pb.view.Backup, "PBServer.ForwardPut", &forward, &fw_reply)
  if !ok {
    fmt.Printf("Failed to call PBServer.ForwardPut on backup '%s': (%s, %s)\n", 
               pb.view.Backup, forward.Key, forward.Value)
    // reply.Err = ErrForwardBackup
  } else {
    if fw_reply.Err == ErrWrongServer {
      fmt.Printf("Forward to the wrong server: '%s'\n", pb.view.Backup)
      reply.Err = ErrForwardBackup
    // } else {
    //   reply.Err = OK
    }
  }

  return nil
}

func (pb *PBServer) SendSnapshot(args *SnapshotArgs, reply *SnapshotReply) error {
  if pb.view.Backup != pb.me {
    fmt.Printf("SendSnapshot: I'm not backup server: '%s'\n", pb.me)
    reply.Err = ErrWrongServer
    return nil
  }
  pb.mu.Lock()
  defer pb.mu.Unlock()
  pb.values = args.Values
  return nil
}

func (pb *PBServer) ForwardPut(args *ForwardPutArgs, reply *ForwardPutReply) error {
  if pb.view.Backup != pb.me {
    fmt.Printf("ForwardPut: I'm not backup server: '%s'\n", pb.me)
    reply.Err = ErrWrongServer
    return nil
  }
  pb.mu.Lock()
  defer pb.mu.Unlock()
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
  view, _ := pb.vs.Ping(pb.view.Viewnum)
  fmt.Printf("%s: Ping %d, p: %s, b: %s\n", pb.me, view.Viewnum, view.Primary, view.Backup)
  pb.mu.Lock()
  defer pb.mu.Unlock()
  update_view := true
  if view.Primary == pb.me {
    if view.Backup != pb.view.Backup {
      snapshot := SnapshotArgs{pb.values}
      var reply SnapshotReply
      ok := call(view.Backup, "PBServer.SendSnapshot", &snapshot, &reply)
      if !ok {
        fmt.Printf("Failed to call PBServer.SendSnapshot on backup: '%s'\n", view.Backup)
      } else if reply.Err == OK {
        fmt.Printf("Successfully send snapshot to backup: '%s'\n", view.Backup)
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
