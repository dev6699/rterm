package provider

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestExpandArgsUsesArgumentValuesWithoutShellParsing(t *testing.T) {
	args, err := expandArgs([]string{"ssh", "{user}@{target}", "echo; touch /tmp/nope"}, map[string]string{
		"user":   "root",
		"target": "node name",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := args[1], "root@node name"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if got, want := args[2], "echo; touch /tmp/nope"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestValidateRejectsIncompleteProfile(t *testing.T) {
	err := (Config{Providers: []Profile{{Name: "test", Discover: DiscoveryCommand{Command: Command{Program: "discover"}}, Users: []string{"root"}, Connect: Command{Program: "ssh"}, Upload: Command{Program: "scp"}, Download: Command{Program: "scp"}}}}).Validate()
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestDiscoverTargetsMapsRawJSONAndPredefinedUsers(t *testing.T) {
	directory := t.TempDir()
	program := filepath.Join(directory, "discover")
	if err := os.WriteFile(program, []byte("#!/bin/sh\nprintf '%s' '[{\"spec\":{\"hostname\":\"node-1\"}}]'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	profile := Profile{
		Name:     "test",
		Discover: DiscoveryCommand{Command: Command{Program: program}, TargetPath: "spec.hostname"},
		Users:    []string{"root", "ubuntu"},
	}
	discovery, err := profile.DiscoverTargets(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(discovery.Targets) != 1 || discovery.Targets[0].ID != "node-1" {
		t.Fatalf("unexpected discovery: %+v", discovery)
	}
	if got := discovery.Targets[0].Users; len(got) != 2 || got[1] != "ubuntu" {
		t.Fatalf("unexpected users: %+v", got)
	}
}

func TestResolveUploadPathPreservesOrAddsMatchingExtension(t *testing.T) {
	cases := []struct {
		name     string
		path     string
		filename string
		want     string
	}{
		{name: "folder", path: "/tmp", filename: "archive.tar", want: "/tmp/archive.tar"},
		{name: "trailing slash folder", path: "/tmp/", filename: "archive.tar", want: "/tmp/archive.tar"},
		{name: "matching extension rename", path: "/tmp/renamed.tar", filename: "source.tar", want: "/tmp/renamed.tar"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			got, err := resolveUploadPath(test.path, test.filename)
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("got %q, want %q", got, test.want)
			}
		})
	}
}

func TestResolveUploadPathRejectsMismatchedExtension(t *testing.T) {
	if _, err := resolveUploadPath("/tmp/archive.zip", "archive.tar"); err == nil {
		t.Fatal("expected extension mismatch")
	}
}
