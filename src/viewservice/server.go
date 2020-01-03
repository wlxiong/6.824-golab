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
  current View
  lastPingTime map[string]time.Time
  lastPingViewnum map[string]uint
  acked_viewnum uint
  idle_server string
}

func (vs *ViewServer) update_viewnum() {
	vs.current.Viewnum = vs.current.Viewnum + 1
}

func (vs *ViewServer) current_view() View {
	// fmt.Printf("reply> p: %s, b: %s, viewnum: %d\n",
	// 	vs.current.Primary, vs.current.Backup, vs.current.Viewnum)
	return vs.current
}

//
// server Ping RPC handler.
//
func (vs *ViewServer) Ping(args *PingArgs, reply *PingReply) error {
  vs.mu.Lock()
  defer vs.mu.Unlock()

	// fmt.Printf("args> %s: , viewnum: %d, \n",  args.Me, args.Viewnum)

  // update heartbeat stats
  vs.lastPingTime[args.Me] = time.Now()
  vs.lastPingViewnum[args.Me] = args.Viewnum
  // add an idle server
  if args.Me != vs.current.Primary &&
     args.Me != vs.current.Backup {
    vs.idle_server = args.Me
  }

  if vs.current.Primary == "" {
		// add the first primary server
		if vs.idle_server != "" {
      vs.current.Primary = vs.idle_server
			vs.idle_server = ""
			vs.update_viewnum()
    }
  } else if vs.current.Backup == "" {
		// add a backup server
		vs.replace_backup()
  } else if args.Me == vs.current.Primary && args.Viewnum == 0 {
		// replace primary server if it restarts
		vs.replace_primary()
  } else if args.Me == vs.current.Backup && args.Viewnum == 0 {
		// replace backup server if it restarts
    vs.replace_backup()
  }

	// update view after acknowledge
	if args.Me == vs.current.Primary {
		if args.Viewnum == vs.current.Viewnum {
			vs.acked_viewnum = vs.current.Viewnum
		}
	}
  // send view
	reply.View = vs.current_view()
  return nil
}

// 
// server Get() RPC handler.
//
func (vs *ViewServer) Get(args *GetArgs, reply *GetReply) error {
	reply.View = vs.current
  // fmt.Printf("Server Get p: %s, b: %s, num: %d\n",
  //          reply.View.Primary, reply.View.Backup, reply.View.Viewnum)
  return nil
}

func (vs *ViewServer) replace_primary() bool {
  // primary in each view must acknowledge that view to viewserver
  // viewserver must stay with current view until acknowledged
  // even if the primary seems to have failed
  // no point in proceeding since not acked == backup may not be initialized
  if vs.acked_viewnum != vs.current.Viewnum {
    fmt.Printf("warning: cannot promote a backup that is not up-to-date\n")
    return false
  }

  if vs.current.Backup == "" {
		fmt.Printf("fatal error: need to replace primary but get no backup\n")
		return false
	}

  fmt.Printf("replace primary, old: %s, new: %s, viewnum: %d\n",
    vs.current.Primary, vs.current.Backup, vs.current.Viewnum)
  vs.current.Primary = vs.current.Backup

  if vs.idle_server != "" {
		vs.current.Backup = vs.idle_server
		vs.idle_server = ""
	} else {
		vs.current.Backup = ""
	}
	vs.update_viewnum()
	return true
}

func (vs *ViewServer) replace_backup() bool {
  if vs.idle_server != "" {
		fmt.Printf("replace backup, old: %s, new: %s, viewnum: %d\n",
			vs.current.Backup, vs.idle_server, vs.current.Viewnum)
    vs.current.Backup = vs.idle_server
		vs.idle_server = ""
		vs.update_viewnum()
		return true
	} else {
		vs.current.Backup = ""
		return false
	}
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
