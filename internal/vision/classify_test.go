package vision

import (
	"strings"
	"testing"
)

func TestIsKnownClassifyProfile(t *testing.T) {
	for _, p := range []string{"general", "chart", "diagram", "document", "screenshot", "code"} {
		if !isKnownClassifyProfile(p) {
			t.Errorf("expected %q to be a known classify profile", p)
		}
	}
	for _, p := range []string{"video", "auto", "", "Chart", "GENERAL", "unknown"} {
		if isKnownClassifyProfile(p) {
			t.Errorf("expected %q to NOT be a known classify profile (case-sensitive, image-only)", p)
		}
	}
}

func TestClassifyProfileNames_AllRegisteredImageProfiles(t *testing.T) {
	// Every name in classifyProfileNames must resolve to a registered
	// Profile — otherwise ClassifyImage would return a valid-looking
	// name that Describe then errors on. This is the symmetry check
	// that prevents that drift.
	for _, name := range classifyProfileNames {
		if _, err := GetProfile(name); err != nil {
			t.Errorf("classify lists profile %q but GetProfile errors: %v — registry drift", name, err)
		}
	}
}

func TestProfileNames_IncludesAuto(t *testing.T) {
	names := ProfileNames()
	if !strings.HasPrefix(names, AutoProfile+", ") {
		t.Errorf("ProfileNames()=%q should start with %q so users see the auto option first", names, AutoProfile)
	}
	for _, name := range classifyProfileNames {
		if !strings.Contains(names, name) {
			t.Errorf("ProfileNames()=%q missing %q", names, name)
		}
	}
}
