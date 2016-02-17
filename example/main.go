package main

/*

/
   net/
      tcp/
         clone
         stats
         0/
            ctl
            data
            listen # (let's start without)
            local
            remote
            status

*/

import (
	"fmt"
	"log"
	"net"
	"os"

	"github.com/joushou/net9p"
	"github.com/joushou/qptools/fileserver"
	"github.com/joushou/qptools/fileserver/trees"
)

func main() {
	// Nasty argument handling - I'm too cool to care.
	if len(os.Args) < 2 {
		fmt.Printf("Too few arguments\n")
		return
	}

	addr := os.Args[1]

	// We shut up by default.
	var verbosity = fileserver.Quiet

	// See if the user wanted a different verbosity level (If he does, he's most likely KL).
	if len(os.Args) > 2 {
		switch os.Args[2] {
		case "quiet":
			verbosity = fileserver.Quiet
		case "chatty":
			verbosity = fileserver.Chatty
		case "loud":
			verbosity = fileserver.Loud
		case "obnoxious":
			verbosity = fileserver.Obnoxious
		case "debug":
			verbosity = fileserver.Debug
		default:
			fmt.Printf("Unknown verbosity level %s\n", os.Args[3])
			return
		}
	}

	// We don't really care about them, but we have to set them.
	user := "wee"
	group := "woo"

	root := trees.NewSyntheticDir("net", 0777, user, group)
	root.Add("tcp", net9p.NewTCPDir(user, group))
	root.Add("udp", net9p.NewUDPDir(user, group))
	root.Add("cs", net9p.NewCSFile(user, group))

	l, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("Unable to listen: %v", err)
	}

	log.Printf("Starting ipfs at %s", addr)

	for {
		conn, err := l.Accept()
		if err != nil {
			log.Printf("Error: %v", err)
			return
		}

		f := fileserver.New(conn, root, nil)
		f.Verbosity = verbosity
		go f.Serve()
	}
}
