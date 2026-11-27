package l10n

import "testing"

func TestRussian(t *testing.T) {
	if got := T("ru", "Connect"); got == "Connect" || got == "" {
		t.Fatalf("Connect has no Russian: %q", got)
	}
	if got := T("en", "Connect"); got != "Connect" {
		t.Fatalf("English is the key: %q", got)
	}
	if got := F("ru", "%d ms", 56); got != "56 мс" {
		t.Fatalf("format: %q", got)
	}
	for n, want := range map[int]string{1: "1 сервер", 3: "3 сервера", 5: "5 серверов", 11: "11 серверов", 21: "21 сервер", 22: "22 сервера"} {
		if got := N("ru", "%d servers", n); got != want {
			t.Errorf("N(%d) = %q, want %q", n, got, want)
		}
	}
	if len(ru) < 150 {
		t.Fatalf("only %d strings read", len(ru))
	}
}
