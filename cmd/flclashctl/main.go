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
	"runtime"
	"strings"
	"syscall"

	"github.com/oviron/flclash-desktop/internal/app"
	"github.com/oviron/flclash-desktop/internal/compile"
	"github.com/oviron/flclash-desktop/internal/core"
	"github.com/oviron/flclash-desktop/internal/store"
)

const usage = `usage:
  flclashctl import [-group NAME] [FILE...]   add servers: Throne links or profile JSON, one per line (stdin without FILE)
  flclashctl list                             show servers and policies
  flclashctl select SERVER [-policy NAME]     choose the server (ID or name) and, optionally, the policy;
                                              a running tunnel moves to the new choice
  flclashctl policy import NAME FILE          add a routing policy from a rules file
  flclashctl policy remove NAME               delete an imported policy
  flclashctl on | off | status                turn the tunnel on or off, or show what is true right now
  flclashctl protect on | off                 whether traffic may leave past the tunnel, also when the core dies
  flclashctl delay [SERVER...]                measure servers one by one: a connection and a small request
  flclashctl helper install | uninstall       set up or remove the privileged helper (run with sudo)
  flclashctl config -tun | -local-port N      print the config the core would run (it contains credentials)
  flclashctl run -local-port N                run the core without privileges on a loopback HTTP/SOCKS port
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
		"on":     cmdOn,
		"off":    cmdOff,
		"status": cmdStatus,
		"delay":  cmdDelay,
		"protect": func(st *store.Store, args []string) error {
			if len(args) != 1 || (args[0] != "on" && args[0] != "off") {
				return errors.New("usage: flclashctl protect on | off")
			}
			return app.Protect(st, args[0] == "on")
		},
	}
	if os.Args[1] == "helper" {
		// Runs as root: it must not create the user's store with root as owner.
		if err := cmdHelper(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "flclashctl:", err)
			os.Exit(1)
		}
		return
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
	result, err := app.Import(st, *group, strings.Join(lines, "\n"))
	for _, server := range result.Imported {
		fmt.Printf("%s  %s\n", server.ID, server.Name)
	}
	for _, failure := range result.Failed {
		// The line itself is not echoed: it holds credentials.
		fmt.Fprintf(os.Stderr, "entry %d: not imported: %s\n", failure.Entry, failure.Reason)
	}
	if err != nil {
		return err
	}
	if len(result.Failed) > 0 {
		return fmt.Errorf("%d of %d entries were not imported", len(result.Failed), len(result.Failed)+len(result.Imported))
	}
	return nil
}

func cmdList(st *store.Store, _ []string) error {
	view, err := app.Snapshot(st)
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
	for _, e := range view.Servers {
		fmt.Printf(" %s %s  %-10s %-22s %s\n", mark(e.ID == view.Selected), e.ID, e.Group, e.Protocol, e.Name)
	}
	fmt.Println("policies:")
	for _, p := range view.Policies {
		fmt.Printf(" %s %s\n", mark(p.Name == view.Policy), p.Name)
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
	return app.Select(st, ref, *policyName)
}

// cmdDelay measures the named servers, or all of them, one after another.
func cmdDelay(st *store.Store, args []string) error {
	if len(args) == 0 {
		view, err := app.Snapshot(st)
		if err != nil {
			return err
		}
		for _, e := range view.Servers {
			args = append(args, e.ID)
		}
	}
	for _, ref := range args {
		name := ref
		if state, err := st.Load(); err == nil {
			if entry, err := state.Find(ref); err == nil {
				name = entry.Name
			}
		}
		if ms, err := app.Delay(st, ref); err != nil {
			fmt.Printf("%-22s failed: %v\n", name, err)
		} else {
			fmt.Printf("%-22s %d ms\n", name, ms)
		}
	}
	return nil
}

func cmdPolicy(st *store.Store, args []string) error {
	if len(args) == 2 && args[0] == "remove" {
		return app.RemovePolicy(st, args[1])
	}
	if len(args) != 3 || args[0] != "import" {
		return errors.New("usage: flclashctl policy import NAME FILE | policy remove NAME")
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
	tun := flags.Bool("tun", false, "capture the traffic of the whole host")
	if err := flags.Parse(args); err != nil {
		return nil, "", err
	}
	if *port == 0 && !*tun {
		return nil, "", errors.New("say how to capture: -tun or -local-port N")
	}
	if *tun && command == "run" {
		return nil, "", errors.New("the tunnel is run by the helper: flclashctl on")
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
	config, err := compile.Config(compile.Input{Server: entry.Server, Policy: pol, LocalPort: *port, TUN: *tun, OS: runtime.GOOS})
	summary := fmt.Sprintf("server %s, policy %s", entry.Name, pol.Name)
	if *tun {
		summary += ", TUN"
	}
	if *port != 0 {
		summary += fmt.Sprintf(", 127.0.0.1:%d", *port)
	}
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
