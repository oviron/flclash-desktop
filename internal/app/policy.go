package app

import (
	"strings"

	"github.com/oviron/flclash-desktop/internal/policy"
	"github.com/oviron/flclash-desktop/internal/store"
)

// PolicyDoc is a policy opened for reading or editing.
type PolicyDoc struct {
	Name    string `json:"name"`
	Builtin bool   `json:"builtin"`
	// Text is the rules as they were written.
	Text string `json:"text"`
	// Listed: the rules read as lists, and the lists below say the same as Text.
	Listed bool   `json:"listed"`
	Direct string `json:"direct"`
	Proxy  string `json:"proxy"`
	Block  string `json:"block"`
	// Rest is where everything else goes: PROXY or DIRECT.
	Rest string `json:"rest"`
}

// ReadPolicy opens a policy.
func ReadPolicy(st *store.Store, name string) (PolicyDoc, error) {
	text, err := st.PolicyText(name)
	if err != nil {
		return PolicyDoc{}, err
	}
	_, isBuiltin := policy.Builtin(name)
	doc := PolicyDoc{Name: name, Builtin: isBuiltin, Text: text}
	if parsed, err := policy.Parse(name, text); err == nil {
		if lists, ok := policy.ListsOf(parsed.Rules); ok {
			doc.Listed = true
			doc.Direct = strings.Join(lists.Direct, "\n")
			doc.Proxy = strings.Join(lists.Proxy, "\n")
			doc.Block = strings.Join(lists.Block, "\n")
			doc.Rest = lists.Rest
		}
	}
	return doc, nil
}

// WritePolicy saves a policy from its rules. A tunnel that runs by this
// policy takes the new rules at once.
func WritePolicy(st *store.Store, name, text string) error {
	if err := st.ImportPolicy(name, text); err != nil {
		return err
	}
	state, err := st.Load()
	if err != nil {
		return err
	}
	if state.Policy != name {
		return nil
	}
	if status, err := Status(); err == nil && status.Running {
		_, err = On(st)
		return err
	}
	return nil
}

// WritePolicyLists saves a policy from its lists.
func WritePolicyLists(st *store.Store, name string, lists policy.Lists) error {
	text, err := lists.Text()
	if err != nil {
		return err
	}
	return WritePolicy(st, name, text)
}
