// Package wfp keeps traffic from leaving a Windows host past the tunnel, with
// filters of the Windows Filtering Platform that live as long as the process
// that added them. The filter plumbing comes from WireGuard for Windows
// (tunnel/firewall, MIT; the copyright notices stay on those files).
package wfp
