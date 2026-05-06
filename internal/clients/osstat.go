package clients

import "os"

// osStat is indirected through a package-level var so tests can fake it
// when needed. Default implementation is os.Stat.
var osStat = os.Stat
