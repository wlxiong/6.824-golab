package viewservice

import "net"
import "net/rpc"
import "log"
import "time"
import "sync"
import "fmt"
import "os"

type ViewServer struct {
  mu sync.Mutex
  l net.Listener
  dead bool
  me string

  // Your declarations here.
  current, pending View
  lastPingTime map[string]time.Time
  lastPingViewnum map[string]uint
  primary_ack bool
  idle_server string
}

//
// server Ping RPC handler.
//
func (vs *ViewServer) Ping(args *PingArgs, reply *PingReply) error {
  vs.mu.Lock()
  defer vs.mu.Unlock()

  // update heartbeat stats
  vs.lastPingTime[args.Me] = time.Now()
  vs.lastPingViewnum[args.Me] = args.Viewnum
  // update view after acknowledge
  if args.Me == vs.pending.Primary &&
     args.Viewnum == vs.pending.Viewnum {
    vs.current = vs.pending
  }
  // initialize view
  if args.Me != vs.pending.Primary &&
     args.Me != vs.pending.Backup {
    vs.idle_server = args.Me
  }
  if vs.pending.Primary == "" {
    if vs.idle_server != "" {
      vs.pending.Primary = vs.idle_server
      vs.pending.Viewnum = vs.current.Viewnum + 1
      vs.idle_server = ""
    }
  }
  if vs.pending.Backup == "" {
    if vs.idle_server != "" {
      vs.pending.Backup = vs.idle_server
      vs.pending.Viewnum = vs.current.Viewnum + 1
      vs.idle_server = ""
    }
  }
  // replace primary/backup server
  if args.Me == vs.current.Primary &&
     args.Viewnum < vs.current.Viewnum {
    vs.replace_primary()
  }
  if args.Me == vs.current.Backup &&
     args.Viewnum < vs.current.Viewnum {
    vs.replace_backup()
  }
  // send view
  if args.Me == vs.pending.Primary {
    reply.View = vs.pending
  } else {
    reply.View = vs.current
  }
  // fmt.Printf("Server Ping p: %s, b: %s, num: %d\n", 
  //            reply.View.Primary, reply.View.Backup, reply.View.Viewnum)
  return nil
}

// 
// server Get() RPC handler.
//
func (vs *ViewServer) Get(args *GetArgs, reply *GetReply) error {
  if args.Me == vs.pending.Primary {
    reply.View = vs.pending
  } else {
    reply.View = vs.current
  }
  // fmt.Printf("Server Get p: %s, b: %s, num: %d\n", 
  //          reply.View.Primary, reply.View.Backup, reply.View.Viewnum)
  return nil
}

func (vs *ViewServer) replace_primary() {
  vs.pending.Primary = vs.current.Backup
  if vs.idle_server != "" {
    vs.pending.Backup = vs.idle_server
    vs.idle_server = ""
  } else {
    vs.pending.Backup = ""
  }
  vs.pending.Viewnum = vs.current.Viewnum + 1
}

func (vs *ViewServer) replace_backup() {
  if vs.idle_server != "" {
    vs.pending.Backup = vs.idle_server
    vs.idle_server = ""
  } else {
    vs.pending.Backup = ""
  }
  vs.pending.Viewnum = vs.current.Viewnum
}

//
// tick() is called once per PingInterval; it should notice
// if servers have died or recovered, and change the view
// accordingly.
//
func (vs *ViewServer) tick() {
  vs.mu.Lock()
  defer vs.mu.Unlock()

  now := time.Now()

  for server, pingTime := range vs.lastPingTime {
    if now.Sub(pingTime) > PingInterval * DeadPings {
      delete(vs.lastPingTime, server)
      delete(vs.lastPingViewnum, server)
      if server == vs.current.Primary {
        vs.replace_primary()
      }
      if server == vs.current.Backup {
        vs.replace_backup()
      }
    }
  }
}

//
// tell the server to shut itself down.
// for testing.
// please don't change this function.
//
func (vs *ViewServer) Kill() {
  vs.dead = true
  vs.l.Close()
}

func StartServer(me string) *ViewServer {
  vs := new(ViewServer)
  vs.me = me
  // Your vs.* initializations here.
  vs.lastPingTime = make(map[string]time.Time)
  vs.lastPingViewnum = make(map[string]uint)

  // tell net/rpc about our RPC server and handlers.
  rpcs := rpc.NewServer()
  rpcs.Register(vs)

  // prepare to receive connections from clients.
  // change "unix" to "tcp" to use over a network.
  os.Remove(vs.me) // only needed for "unix"
  l, e := net.Listen("unix", vs.me);
  if e != nil {
    log.Fatal("listen error: ", e);
  }
  vs.l = l

  // please don't change any of the following code,
  // or do anything to subvert it.

  // create a thread to accept RPC connections from clients.
  go func() {
    for vs.dead == false {
      conn, err := vs.l.Accept()
      if err == nil && vs.dead == false {
        go rpcs.ServeConn(conn)
      } else if err == nil {
        conn.Close()
      }
      if err != nil && vs.dead == false {
        fmt.Printf("ViewServer(%v) accept: %v\n", me, err.Error())
        vs.Kill()
      }
    }
  }()

  // create a thread to call tick() periodically.
  go func() {
    for vs.dead == false {
      vs.tick()
      time.Sleep(PingInterval)
    }
  }()

  return vs
}
