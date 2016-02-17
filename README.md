# net9p

Plan9 ip(3) implementation in Go. It implements TCP and UDP dial and listen, as well as name lookups through cs. An implementation of Go's Dial and Listen is also available in the subpackage "client", allowing use of an ip(3) implementation from Go (which could in theory be a real plan9 box).
