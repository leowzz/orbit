package codex

import "testing"

func TestThreadURLMatchesCodexNotifyFormat(t *testing.T) {
	t.Parallel()
	const sessionID = "01a066af-69d4-77d1-a21b-26d84534a817"
	url, err := ThreadURL(sessionID)
	if err != nil {
		t.Fatal(err)
	}
	if want := "codex://threads/" + sessionID; url != want {
		t.Fatalf("ThreadURL() = %q, want %q", url, want)
	}
}

func TestThreadURLRejectsNonUUID(t *testing.T) {
	t.Parallel()
	if _, err := ThreadURL("../../Applications/Calculator.app"); err == nil {
		t.Fatal("ThreadURL accepted a non-UUID session ID")
	}
}
