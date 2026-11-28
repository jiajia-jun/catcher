package app

import "testing"

func TestDispatch(t *testing.T) {
	cases := []struct {
		args []string
		want Entry
	}{
		{nil, EntryGUI},
		{[]string{"refresh"}, EntryCLI},
		{[]string{"list"}, EntryCLI},
		{[]string{"status"}, EntryCLI},
		{[]string{"mark"}, EntryCLI},
		{[]string{"version"}, EntryCLI},
		{[]string{"help"}, EntryCLI},
		{[]string{"--help"}, EntryCLI},
		{[]string{"-h"}, EntryCLI},
		{[]string{"--version"}, EntryCLI},
		{[]string{"--data-dir", "D:\\data"}, EntryGUI},
		{[]string{"--data-dir=D:\\data"}, EntryGUI},
		{[]string{"bogus"}, EntryUsage},
		{[]string{"--nope"}, EntryUsage},
	}
	for _, c := range cases {
		if got := Dispatch(c.args); got != c.want {
			t.Errorf("Dispatch(%v) = %v, want %v", c.args, got, c.want)
		}
	}
}

func TestExtractDataDirArgs(t *testing.T) {
	got, err := extractDataDirArgs([]string{"list", "--data-dir", "D:\\data", "--status", "submitted"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"--data-dir", "D:\\data"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("参数 = %v, want %v", got, want)
	}

	got, err = extractDataDirArgs([]string{"--data-dir=D:\\data"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "--data-dir=D:\\data" {
		t.Fatalf("参数 = %v", got)
	}

	if _, err := extractDataDirArgs([]string{"--data-dir"}); err == nil {
		t.Fatal("缺少路径时应报错")
	}
}

func TestHasVerbose(t *testing.T) {
	if !hasVerbose([]string{"refresh", "--verbose"}) {
		t.Fatal("应识别 --verbose")
	}
	if hasVerbose([]string{"refresh", "--verbose=false"}) {
		t.Fatal("--verbose=false 不应开启")
	}
}
