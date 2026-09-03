package metrics

import (
	"runtime/debug"
	"testing"
)

// The plane is built two ways and only one of them stamps vcs.revision.
// `make plane-image` builds from a checkout through plane/Dockerfile, which
// passes -X because the build context has no .git; `kmx plane` builds the
// module straight from the Go proxy, where the toolchain records
// Main.Version and NO vcs settings at all. Before this table the second case
// published kaimahi_build_info{revision="unknown"} — on the one path where
// there is no checkout to compare the running plane against.
func TestVersionReadsBothStampings(t *testing.T) {
	vcs := func(rev string, dirty bool) []debug.BuildSetting {
		s := []debug.BuildSetting{{Key: "vcs.revision", Value: rev}}
		if dirty {
			s = append(s, debug.BuildSetting{Key: "vcs.modified", Value: "true"})
		}
		return s
	}
	for _, tc := range []struct {
		name     string
		main     string
		settings []debug.BuildSetting
		want     string
	}{
		{
			name: "module proxy build (kmx plane): the revision is in the pseudo-version",
			main: "v0.0.0-20260903013736-ffed1ee20737",
			want: "ffed1ee20737",
		},
		{
			name:     "checkout build: vcs.revision, shortened",
			main:     "(devel)",
			settings: vcs("ffed1ee20737abcdef0123456789abcdef012345", false),
			want:     "ffed1ee20737",
		},
		{
			name:     "a dirty checkout still says so",
			main:     "(devel)",
			settings: vcs("ffed1ee20737abcdef0123456789abcdef012345", true),
			want:     "ffed1ee20737-dirty",
		},
		{
			name:     "vcs wins over the module version when both are present",
			main:     "v0.0.0-20260903013736-aaaaaaaaaaaa",
			settings: vcs("ffed1ee20737", false),
			want:     "ffed1ee20737",
		},
		{
			name: "a tagged release names itself",
			main: "v1.2.3",
			want: "v1.2.3",
		},
		{
			// Shortening is for a 40-character sha, not for a version.
			// Truncated to 12 this would read "v0.1.0-beta." and name no
			// release at all.
			name: "a long tag is not shortened into nonsense",
			main: "v0.1.0-beta.1",
			want: "v0.1.0-beta.1",
		},
		{
			// A pre-release suffix is not a revision; taking the last field
			// unconditionally would publish revision="rc1".
			name: "a pre-release tag is not mistaken for a revision",
			main: "v1.2.3-rc1",
			want: "v1.2.3-rc1",
		},
		{
			name: "an unstamped local build is honest about it",
			main: "(devel)",
			want: "unknown",
		},
		{
			name: "no build info at all",
			want: "unknown",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := versionFrom(tc.main, tc.settings); got != tc.want {
				t.Errorf("versionFrom(%q, %v) = %q, want %q", tc.main, tc.settings, got, tc.want)
			}
		})
	}
}

// The linker-set value still wins over everything: that is the path
// plane/Dockerfile uses, and it is the most direct statement of all.
func TestLinkerStampWins(t *testing.T) {
	buildVersion = "deadbeefcafe"
	t.Cleanup(func() { buildVersion = "" })
	if got := Version(); got != "deadbeefcafe" {
		t.Errorf("Version() = %q with a linker stamp set", got)
	}
}
