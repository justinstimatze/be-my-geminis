package vision

import (
	"fmt"
	"sort"
	"strings"

	"google.golang.org/genai"
)

// Profile bundles the prompt and response schema that drive Gemini's
// vision call for one analysis style. Prompt + schema are paired so the
// structured output Gemini returns matches the prompt's terminology and
// field expectations exactly. The pattern is schema-per-profile rather
// than one mega-schema with optional fields because Gemini populates
// every declared field — "optional" means "can be null", not "model
// decides whether to emit" — which would waste tokens on profiles that
// don't need a given field.
type Profile struct {
	Name   string
	Prompt string
	Schema *genai.Schema
	// SystemInstruction is an optional role/behavior primer that lands
	// in Gemini's system_instruction slot, separate from the user
	// message. Empty = no system instruction (the original pre-profile
	// behavior).
	// Profiles use this to enforce role consistency ("you are
	// extracting structured chart data; respect the schema exactly;
	// cite specific numeric values where visible") without bloating
	// the user-message prompt.
	SystemInstruction string
}

// Profiles is the registry mapping profile name → (prompt, schema).
// Populated by init() in each profile_*.go file so adding a profile is
// a single-file change.
//
// Concurrency: Profiles is finalized at package-init time and treated
// as read-only thereafter. registerProfile must only be called from
// init(); calling it from a goroutine post-init would race with map
// reads at GetProfile call sites and is not supported.
var Profiles = map[string]Profile{}

// registerProfile adds p to the Profiles map. Call only from init() in
// a profile_*.go file — see Profiles' concurrency note.
func registerProfile(p Profile) {
	Profiles[p.Name] = p
}

// GetProfile resolves a name to a profile. Empty name means "use the
// default" (general). An unknown non-empty name is an error so callers
// (MCP describe, bmg describe CLI) can surface it instead of silently
// using the general fallback — better to fail loud than mis-route.
func GetProfile(name string) (Profile, error) {
	if name == "" {
		name = "general"
	}
	p, ok := Profiles[name]
	if !ok {
		return Profile{}, fmt.Errorf("unknown profile %q (available: %s)", name, ProfileNames())
	}
	return p, nil
}

// ProfileNames returns the registered profile names, sorted, for help
// text and error messages. Includes the AutoProfile sentinel so
// callers that surface this string (--help text, MCP inputSchema
// description) advertise that "auto" is a valid choice.
func ProfileNames() string {
	names := make([]string, 0, len(Profiles)+1)
	for n := range Profiles {
		names = append(names, n)
	}
	sort.Strings(names)
	// Surface "auto" first since it's the recommended default for
	// callers that don't know which profile fits.
	return AutoProfile + ", " + strings.Join(names, ", ")
}
