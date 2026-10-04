package main

import "testing"

func TestPlatformString(t *testing.T) {
	p := Platform{OS: "linux", Arch: "amd64"}
	if got := p.String(); got != "linux/amd64" {
		t.Errorf("String() = %q, want %q", got, "linux/amd64")
	}
}

func TestMatchAny(t *testing.T) {
	tests := []struct {
		patterns []string
		p        Platform
		want     bool
	}{
		{[]string{"linux/amd64"}, Platform{"linux", "amd64"}, true},
		{[]string{"linux/*"}, Platform{"linux", "arm64"}, true},
		{[]string{"*/arm64"}, Platform{"linux", "arm64"}, true},
		{[]string{"*/*"}, Platform{"freebsd", "riscv64"}, true},
		{[]string{"windows/amd64"}, Platform{"linux", "amd64"}, false},
		{[]string{"linux/arm"}, Platform{"linux", "arm64"}, false},
		{[]string{"linux/amd64", "darwin/*"}, Platform{"darwin", "arm64"}, true},
		{nil, Platform{"linux", "amd64"}, false},
	}
	for _, tt := range tests {
		got := matchAny(tt.p, tt.patterns)
		if got != tt.want {
			t.Errorf("matchAny(%v, %v) = %v, want %v", tt.patterns, tt.p, got, tt.want)
		}
	}
}

func TestFilterPlatforms(t *testing.T) {
	platforms := []Platform{
		{"linux", "amd64"},
		{"linux", "arm64"},
		{"darwin", "amd64"},
		{"darwin", "arm64"},
		{"windows", "amd64"},
		{"freebsd", "amd64"},
	}

	tests := []struct {
		name    string
		include string
		exclude string
		want    []Platform
	}{
		{
			name:    "all platforms",
			include: "",
			exclude: "",
			want:    platforms,
		},
		{
			name:    "include linux only",
			include: "linux/*",
			exclude: "",
			want:    []Platform{{"linux", "amd64"}, {"linux", "arm64"}},
		},
		{
			name:    "exclude arm",
			include: "",
			exclude: "*/arm64",
			want:    []Platform{{"linux", "amd64"}, {"darwin", "amd64"}, {"windows", "amd64"}, {"freebsd", "amd64"}},
		},
		{
			name:    "include and exclude",
			include: "linux/* darwin/*",
			exclude: "*/arm64",
			want:    []Platform{{"linux", "amd64"}, {"darwin", "amd64"}},
		},
		{
			name:    "no match",
			include: "nonexistent/*",
			exclude: "",
			want:    nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Config{
				platformInclude: tt.include,
				platformExclude: tt.exclude,
			}
			got := c.filterPlatforms(platforms)
			if len(got) != len(tt.want) {
				t.Fatalf("filterPlatforms() = %v (%d), want %v (%d)", got, len(got), tt.want, len(tt.want))
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("filterPlatforms() = %v, want %v", got, tt.want)
				}
			}
		})
	}
}

func TestValidatePatterns(t *testing.T) {
	tests := []struct {
		name    string
		include string
		exclude string
		wantErr bool
	}{
		{name: "valid patterns", include: "linux/amd64 windows/*", exclude: "*/arm"},
		{name: "empty patterns", include: "", exclude: ""},
		{name: "unterminated class in include", include: "linux/[amd64", wantErr: true},
		{name: "unterminated class in exclude", exclude: "[", wantErr: true},
		{name: "valid alongside invalid", include: "linux/amd64 bad[", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validatePatterns(tt.include, tt.exclude)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validatePatterns(%q, %q) error = %v, wantErr %v", tt.include, tt.exclude, err, tt.wantErr)
			}
		})
	}
}
