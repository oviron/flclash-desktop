// flclashctl manages servers and routing policies and runs the core.
package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/oviron/flclash-desktop/internal/compile"
	"github.com/oviron/flclash-desktop/internal/core"
	"github.com/oviron/flclash-desktop/internal/ingest"
	"github.com/oviron/flclash-desktop/internal/store"
)

const usage = `usage:
  flclashctl import [-group NAME] [FILE...]   add servers: Throne links or profile JSON, one per line (stdin without FILE)
  flclashctl list                             show servers and policies
  flclashctl select SERVER [-policy NAME]     choose the server (ID or name) and, optionally, the policy
  flclashctl policy import NAME FILE          add a routing policy from a rules file
  flclashctl config -local-port N             print the config the core would run (it contains credentials)
  flclashctl run -local-port N                run the core with the current choice until interrupted
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	commands := map[string]func(*store.Store, []string) error{
		"import": cmdImport,
		"list":   cmdList,
		"select": cmdSelect,
		"policy": cmdPolicy,
		"config": cmdConfig,
		"run":    cmdRun,
	}
	command, ok := commands[os.Args[1]]
	if !ok {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	st, err := store.Open()
	if err == nil {
		err = command(st, os.Args[2:])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "flclashctl:", err)
		os.Exit(1)
	}
}

func cmdImport(st *store.Store, args []string) error {
	flags := flag.NewFlagSet("import", flag.ContinueOnError)
	group := flags.String("group", "default", "group to put the servers in")
	if err := flags.Parse(args); err != nil {
		return err
	}
	var lines []string
	read := func(r io.Reader) error {
		scanner := bufio.NewScanner(r)
		scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
		for scanner.Scan() {
			if line := strings.TrimSpace(scanner.Text()); line != "" {
				lines = append(lines, line)
			}
		}
		return scanner.Err()
	}
	if flags.NArg() == 0 {
		if err := read(os.Stdin); err != nil {
			return err
		}
	}
	for _, path := range flags.Args() {
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		err = read(f)
		f.Close()
		if err != nil {
			return err
		}
	}
	state, err := st.Load()
	if err != nil {
		return err
	}
	failed := 0
	for i, line := range lines {
		server, err := ingest.FromThrone(line)
		if err != nil {
			failed++
			// The line itself is not echoed: it holds credentials.
			fmt.Fprintf(os.Stderr, "entry %d: not imported: %v\n", i+1, err)
			continue
		}
		entry := state.Put(*group, server)
		fmt.Printf("%s  %s\n", entry.ID, entry.Name)
	}
	if err := st.Save(state); err != nil {
		return err
	}
	if failed > 0 {
		return fmt.Errorf("%d of %d entries were not imported", failed, len(lines))
	}
	return nil
}

func cmdList(st *store.Store, _ []string) error {
	state, err := st.Load()
	if err != nil {
		return err
	}
	mark := func(on bool) string {
		if on {
			return "*"
		}
		return " "
	}
	fmt.Println("servers:")
	for _, e := range state.Servers {
		fmt.Printf(" %s %s  %-10s %-9s %s\n", mark(e.ID == state.Selected), e.ID, e.Group, e.Proxy["type"], e.Name)
	}
	names, err := st.PolicyNames()
	if err != nil {
		return err
	}
	fmt.Println("policies:")
	for _, name := range names {
		fmt.Printf(" %s %s\n", mark(name == state.Policy), name)
	}
	return nil
}

func cmdSelect(st *store.Store, args []string) error {
	flags := flag.NewFlagSet("select", flag.ContinueOnError)
	policyName := flags.String("policy", "", "routing policy to switch to")
	// Accept the flag on either side of the server name.
	var ref string
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		ref, args = args[0], args[1:]
	}
	if err := flags.Parse(args); err != nil {
		return err
	}
	if ref == "" && flags.NArg() == 1 {
		ref = flags.Arg(0)
	}
	if ref == "" && *policyName == "" {
		return errors.New("name a server, a policy, or both")
	}
	state, err := st.Load()
	if err != nil {
		return err
	}
	if ref != "" {
		entry, err := state.Find(ref)
		if err != nil {
			return err
		}
		state.Selected = entry.ID
	}
	if *policyName != "" {
		if _, err := st.Policy(*policyName); err != nil {
			return err
		}
		state.Policy = *policyName
	}
	return st.Save(state)
}

func cmdPolicy(st *store.Store, args []string) error {
	if len(args) != 3 || args[0] != "import" {
		return errors.New("usage: flclashctl policy import NAME FILE")
	}
	text, err := os.ReadFile(args[2])
	if err != nil {
		return err
	}
	return st.ImportPolicy(args[1], string(text))
}

// current compiles the config for the selected server and policy.
func current(st *store.Store, args []string, command string) ([]byte, string, error) {
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	port := flags.Int("local-port", 0, "loopback port of the HTTP/SOCKS listener")
	if err := flags.Parse(args); err != nil {
		return nil, "", err
	}
	if *port == 0 {
		// Capture through TUN needs the privileged helper, which is not built yet.
		return nil, "", errors.New("-local-port is required: this build has no TUN yet")
	}
	state, err := st.Load()
	if err != nil {
		return nil, "", err
	}
	if state.Selected == "" {
		return nil, "", errors.New("no server is selected: flclashctl select SERVER")
	}
	entry, err := state.Find(state.Selected)
	if err != nil {
		return nil, "", err
	}
	pol, err := st.Policy(state.Policy)
	if err != nil {
		return nil, "", err
	}
	config, err := compile.Config(compile.Input{Server: entry.Server, Policy: pol, LocalPort: *port})
	summary := fmt.Sprintf("server %s, policy %s, 127.0.0.1:%d", entry.Name, pol.Name, *port)
	return config, summary, err
}

func cmdConfig(st *store.Store, args []string) error {
	config, _, err := current(st, args, "config")
	if err != nil {
		return err
	}
	_, err = os.Stdout.Write(config)
	return err
}

func cmdRun(st *store.Store, args []string) error {
	config, summary, err := current(st, args, "run")
	if err != nil {
		return err
	}
	if err := core.Apply(st.CoreDir(), config); err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, "running:", summary)
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
	core.Stop()
	return nil
}
