package helper

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/oviron/flclash-desktop/internal/say"
)

// hidden starts a program with no window: the helper is a service, and what
// it runs as the user must not flash a console on the user's desktop.
func hidden() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
}

// userToken finds the session of the user the helper serves and returns a
// token to start programs in it. The user has to be signed in.
func userToken(sid string) (windows.Token, error) {
	var sessions *windows.WTS_SESSION_INFO
	var count uint32
	if err := windows.WTSEnumerateSessions(0, 0, 1, &sessions, &count); err != nil {
		return 0, fmt.Errorf("WTSEnumerateSessions: %w", err)
	}
	defer windows.WTSFreeMemory(uintptr(unsafe.Pointer(sessions)))
	list := unsafe.Slice(sessions, count)
	var fallback windows.Token
	for _, session := range list {
		var token windows.Token
		if err := windows.WTSQueryUserToken(session.SessionID, &token); err != nil {
			continue
		}
		user, err := token.GetTokenUser()
		if err != nil || user.User.Sid.String() != sid {
			token.Close()
			continue
		}
		if session.State == windows.WTSActive {
			if fallback != 0 {
				fallback.Close()
			}
			return token, nil
		}
		if fallback == 0 {
			fallback = token
		} else {
			token.Close()
		}
	}
	if fallback != 0 {
		return fallback, nil
	}
	return 0, say.New("the user the helper serves is not signed in")
}

// asUser makes cmd run as the user, in the user's session, with the user's
// environment. The caller closes the token once the program has started.
func asUser(cmd *exec.Cmd, sid string) (windows.Token, error) {
	token, err := userToken(sid)
	if err != nil {
		return 0, err
	}
	env, err := token.Environ(false)
	if err != nil {
		token.Close()
		return 0, err
	}
	cmd.Env = env
	attr := hidden()
	attr.Token = syscall.Token(token)
	cmd.SysProcAttr = attr
	return token, nil
}

// runAsUser runs a program as the user and returns what it printed.
func runAsUser(sid string, stdin []byte, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	token, err := asUser(cmd, sid)
	if err != nil {
		return nil, err
	}
	defer token.Close()
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) && stderr.Len() > 0 {
			return out, errors.New(strings.TrimSpace(stderr.String()))
		}
		return out, err
	}
	return out, nil
}

// secure gives a directory to the system and administrators alone, and to the
// user the helper serves the rights named: "" for none, "read", or "full".
// What the directory held before keeps no other rights.
func secure(path, sid, rights string) error {
	if err := os.MkdirAll(path, 0o700); err != nil {
		return err
	}
	sddl := "O:SYD:P(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)"
	switch rights {
	case "read":
		sddl += "(A;OICI;FR;;;" + sid + ")"
	case "full":
		sddl += "(A;OICI;FA;;;" + sid + ")"
	}
	sd, err := windows.SecurityDescriptorFromString(sddl)
	if err != nil {
		return err
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		return err
	}
	owner, _, err := sd.Owner()
	if err != nil {
		return err
	}
	info := windows.SECURITY_INFORMATION(windows.DACL_SECURITY_INFORMATION | windows.PROTECTED_DACL_SECURITY_INFORMATION)
	if err := windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, info|windows.OWNER_SECURITY_INFORMATION, owner, nil, dacl, nil); err != nil {
		// Taking ownership may be refused; the rights matter most.
		if err := windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, info, nil, nil, dacl, nil); err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
	}
	return nil
}

// PrepareDirs gives the helper's directories their rights: the user it serves
// reads the log, and only the system touches the files of the core that runs
// as the system.
func PrepareDirs(sid string) error {
	if err := secure(BaseDir(), sid, "read"); err != nil {
		return err
	}
	return secure(StateDir(), sid, "")
}

// PipeSDDL lets the system and the user the helper serves open the pipe, to
// read and write and nothing else: no one else may give it orders, and not
// even that user may stand up a pipe of the same name.
func PipeSDDL(sid string) string {
	return "D:P(A;;GA;;;SY)(A;;0x12019b;;;" + sid + ")"
}
