package viewservice

import "testing"
import "runtime"
import "time"
import "fmt"
import "os"
import "strconv"

func check(t *testing.T, ck *Clerk, p string, b string, n uint) {
  view, _ := ck.Get()
  if view.Primary != p {
    t.Fatalf("wanted primary %v, got %v", p, view.Primary)
  }
  if view.Backup != b {
    t.Fatalf("wanted backup %v, got %v", b, view.Backup)
  }
  if n != 0 && n != view.Viewnum {
    t.Fatalf("wanted viewnum %v, got %v", n, view.Viewnum)
  }
  if ck.Primary() != p {
    t.Fatalf("wanted primary %v, got %v", p, ck.Primary())
  }
}

func port(suffix string) string {
  s := "/var/tmp/824-"
  s += strconv.Itoa(os.Getuid()) + "/"
  os.Mkdir(s, 0777)
  s += "viewserver-" 
  s += strconv.Itoa(os.Getpid()) + "-"
  s += suffix
  return s
}

func Test1(t *testing.T) {
  runtime.GOMAXPROCS(4)

  vshost := port("v")
  vs := StartServer(vshost)

  ck1 := MakeClerk(port("1"), vshost)
  ck2 := MakeClerk(port("2"), vshost)
  ck3 := MakeClerk(port("3"), vshost)

  //

  if ck1.Primary() != "" {
    t.Fatalf("there was a primary too soon")
  }

  // very first primary
  fmt.Printf("Test: First primary ...\n")

  for i := 0; i < DeadPings * 2; i++ {
    view, _ := ck1.Ping(0)
    if view.Primary == ck1.me {
      break
    }
    time.Sleep(PingInterval)
  }
  check(t, ck1, ck1.me, "", 1)
  fmt.Printf("  ... Passed\n")

  // very first backup
  fmt.Printf("Test: First backup ...\n")

  {
    vx, _ := ck1.Get()
		// vs.Viewnum = 1
    for i := 0; i < DeadPings * 2; i++ {
      ck1.Ping(1)
			// current viewnum: 1
      view, _ := ck2.Ping(0)
			// pending viewnum: 2
      if view.Backup == ck2.me {
				fmt.Printf("new backup: %s\n", ck2.me)
        break
      }
      time.Sleep(PingInterval)
    }
		v, _ := ck1.Get()
		fmt.Printf("p: %s, b: %s, Viewnum: %d\n", v.Primary, v.Backup, v.Viewnum)
		fmt.Printf("ck1: %s, ck2: %s, vx.Viewnum: %d\n", ck1.me, ck2.me, vx.Viewnum)
    check(t, ck1, ck1.me, ck2.me, vx.Viewnum + 1)
  }
  fmt.Printf("  ... Passed\n")

  // primary dies, backup should take over
  fmt.Printf("Test: Backup takes over if primary fails ...\n")

  {
    ck1.Ping(2)
		// current view = 2, p: ck1, b: ck2
    vx, _ := ck2.Ping(2)
		// vx.Viewnum = 2
    for i := 0; i < DeadPings * 2; i++ {
      v, _ := ck2.Ping(vx.Viewnum)
			// v.Viewnum = 2, p: ck2, b: ck1
      if v.Primary == ck2.me && v.Backup == "" {
				// v.Viewnum = 3, p: ck2, b: ""
				// if a backup becomes primary, viewnum increases by one
        break
      }
      time.Sleep(PingInterval)
    }
    check(t, ck2, ck2.me, "", vx.Viewnum + 1)
  }
  fmt.Printf("  ... Passed\n")

  // revive ck1, should become backup
  fmt.Printf("Test: Restarted server becomes backup ...\n")

  {
    vx, _ := ck2.Get()
		// vx.Viewnum = 3, p: ck2, b: ""
    ck2.Ping(vx.Viewnum)
    for i := 0; i < DeadPings * 2; i++ {
      ck1.Ping(0)
      v, _ := ck2.Ping(vx.Viewnum)
			// v.Viewnum = 4, p: ck2, b: ck1
      if v.Primary == ck2.me && v.Backup == ck1.me {
        break
      }
      time.Sleep(PingInterval)
    }
    check(t, ck2, ck2.me, ck1.me, vx.Viewnum + 1)
  }
  fmt.Printf("  ... Passed\n")

  // start ck3, kill the primary (ck2), the previous backup (ck1)
  // should become the server, and ck3 the backup
  fmt.Printf("Test: Idle third server becomes backup if primary fails ...\n")

  {
    vx, _ := ck2.Get()
		// vx.Viewnum = 4, p: ck2, b: ck1
    ck2.Ping(vx.Viewnum)
    for i := 0; i < DeadPings * 2; i++ {
      ck3.Ping(0)
      v, _ := ck1.Ping(vx.Viewnum)
			// v.Viewnum = 5, p: ck1, b: ck3
      if v.Primary == ck1.me && v.Backup == ck3.me {
        break;
      }
      vx = v
      time.Sleep(PingInterval)
    }
    check(t, ck1, ck1.me, ck3.me, vx.Viewnum + 1)
  }
  fmt.Printf("  ... Passed\n")

  // kill and immediately restart the primary -- does viewservice
  // conclude primary is down even though it's pinging?
  fmt.Printf("Test: Restarted primary treated as dead ...\n")

  {
    vx, _ := ck1.Get()
		// vx.Viewnum = 5, p: ck1, b: ck3
    ck1.Ping(vx.Viewnum)
    for i := 0; i < DeadPings * 2; i++ {
      ck1.Ping(0)
      ck3.Ping(vx.Viewnum)
      v, _ := ck3.Get()
      if v.Primary != ck1.me {
				// v.Viewnum = 6, p: ck3, b: ""
        break
      }
      time.Sleep(PingInterval)
    }
    vy, _ := ck3.Get()
    if vy.Primary != ck3.me {
      t.Fatalf("expected primary=%v, got %v\n", ck3.me, vy.Primary)
    }
  }
  fmt.Printf("  ... Passed\n")
  

  // set up a view with just 3 as primary,
  // to prepare for the next test.
  {
    for i := 0; i < DeadPings * 3; i++ {
      vx, _ := ck3.Get()
      ck3.Ping(vx.Viewnum)
      time.Sleep(PingInterval)
    }
    v, _ := ck3.Get()
    if v.Primary != ck3.me || v.Backup != "" {
      t.Fatalf("wrong primary or backup")
    }
  }

  // does viewserver wait for ack of previous view before
  // starting the next one?
  fmt.Printf("Test: Viewserver waits for primary to ack view ...\n")
  
  {
    // set up p=ck3 b=ck1, but
    // but do not ack
    vx, _ := ck1.Get()
		// vx.Viewnum = 6, p: ck3, b: ""
    for i := 0; i < DeadPings * 3; i++ {
      ck1.Ping(0)
      ck3.Ping(vx.Viewnum)
      v, _ := ck1.Get()
      if v.Viewnum > vx.Viewnum {
				fmt.Printf("new viewnum\n")
        break
      }
      time.Sleep(PingInterval)
    }
		fmt.Printf("ck3.me: %s, ck1.me: %s\n", ck3.me, ck1.me)
    check(t, ck1, ck3.me, ck1.me, vx.Viewnum+1)
    vy, _ := ck1.Get()
		fmt.Printf("p: %s, b: %s\n", vy.Primary, vy.Backup)
    // ck3 is the primary, but it never acked.
    // let ck3 die. check that ck1 is not promoted.
    for i := 0; i < DeadPings * 3; i++ {
      v, _ := ck1.Ping(vy.Viewnum)
      if v.Viewnum > vy.Viewnum {
				fmt.Printf("error: viewnum advanced without ackknowledge\n")
        break
      }
      time.Sleep(PingInterval)
    }
    check(t, ck2, ck3.me, ck1.me, vy.Viewnum)
  }
  fmt.Printf("  ... Passed\n")

  // if old servers die, check that a new (uninitialized) server
  // cannot take over.
  fmt.Printf("Test: Uninitialized server can't become primary ...\n")

  {
    for i := 0; i < DeadPings * 2; i++ {
      v, _ := ck1.Get()
      ck1.Ping(v.Viewnum)
      ck2.Ping(0)
      ck3.Ping(v.Viewnum)
      time.Sleep(PingInterval)
    }
    for i := 0; i < DeadPings * 2; i++ {
      ck2.Ping(0)
      time.Sleep(PingInterval)
    }
    vz, _ := ck2.Get()
    if vz.Primary == ck2.me {
      t.Fatalf("uninitialized backup promoted to primary")
    }
  }
  fmt.Printf("  ... Passed\n")

  vs.Kill()
}
