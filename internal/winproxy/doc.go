// Package winproxy reads and sets the proxy of the Windows user it runs as,
// the one browsers and most programs follow (WinINet). It has to run as that
// user: the setting lives in the user's profile, and the programs that
// follow it hear about a change only from the user's own session.
package winproxy
