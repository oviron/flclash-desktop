package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"

	"github.com/oviron/flclash-desktop/internal/winproxy"
)

// otherMode handles -sysproxy: the helper starts the core as the user to read
// or change the user's proxy setting, which a service cannot do itself.
//
//	flclash-core -sysproxy get           prints the setting as JSON
//	flclash-core -sysproxy set PORT      points it at the loopback port
//	flclash-core -sysproxy restore       puts back the JSON on stdin
func otherMode() bool {
	if len(os.Args) < 3 || os.Args[1] != "-sysproxy" {
		return false
	}
	if err := sysproxy(os.Args[2:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	return true
}

func sysproxy(args []string) error {
	switch {
	case args[0] == "get":
		setting, err := winproxy.Read()
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(setting)
	case args[0] == "set" && len(args) == 2:
		port, err := strconv.Atoi(args[1])
		if err != nil || port < 1 || port > 65535 {
			return fmt.Errorf("bad port %q", args[1])
		}
		return winproxy.Point(port)
	case args[0] == "restore":
		var setting winproxy.Setting
		if err := json.NewDecoder(io.LimitReader(os.Stdin, 1<<16)).Decode(&setting); err != nil {
			return err
		}
		return winproxy.Write(setting)
	}
	return fmt.Errorf("usage: -sysproxy get | set PORT | restore")
}
