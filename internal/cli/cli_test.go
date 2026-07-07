package cli

import "testing"

func TestParseGitStatArgs(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		jobs    int
		folders []string
	}{
		{
			name:    "folders only",
			args:    []string{"repo-a", "repo-b"},
			folders: []string{"repo-a", "repo-b"},
		},
		{
			name:    "jobs before folders",
			args:    []string{"--jobs", "4", "repo-a", "repo-b"},
			jobs:    4,
			folders: []string{"repo-a", "repo-b"},
		},
		{
			name:    "jobs equals",
			args:    []string{"repo-a", "--jobs=2", "repo-b"},
			jobs:    2,
			folders: []string{"repo-a", "repo-b"},
		},
		{
			name:    "option terminator",
			args:    []string{"--jobs", "3", "--", "--repo"},
			jobs:    3,
			folders: []string{"--repo"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts, folders, err := parseGitStatArgs(tt.args)
			if err != nil {
				t.Fatal(err)
			}
			if opts.Jobs != tt.jobs {
				t.Fatalf("Jobs = %d, want %d", opts.Jobs, tt.jobs)
			}
			if len(folders) != len(tt.folders) {
				t.Fatalf("folders = %v, want %v", folders, tt.folders)
			}
			for idx := range folders {
				if folders[idx] != tt.folders[idx] {
					t.Fatalf("folders = %v, want %v", folders, tt.folders)
				}
			}
		})
	}
}

func TestParseGitStatArgsErrors(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "missing folders", args: nil},
		{name: "missing jobs value", args: []string{"--jobs"}},
		{name: "zero jobs", args: []string{"--jobs", "0", "repo"}},
		{name: "invalid jobs", args: []string{"--jobs", "nope", "repo"}},
		{name: "unknown option", args: []string{"--color", "repo"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, _, err := parseGitStatArgs(tt.args); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}
