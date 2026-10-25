//go:build windows

package main

import "golang.org/x/sys/windows"

// privileged reports whether yamn runs as an administrator: elevated (run as
// administrator, or as SYSTEM), or with the Administrators group active in
// its token, as when UAC is off.  An administrator's ordinary process under
// UAC has the group only for deny, and may run.
func privileged() (string, bool) {
	if windows.GetCurrentProcessToken().IsElevated() {
		return "an administrator", true
	}
	admins, err := windows.CreateWellKnownSid(windows.WinBuiltinAdministratorsSid)
	if err != nil {
		return "", false
	}
	// Token 0 checks the token of the calling thread.
	member, err := windows.Token(0).IsMember(admins)
	return "an administrator", err == nil && member
}
