package vision

import (
	"strings"
	"testing"

	"google.golang.org/genai"
)

// stubProfile fabricates a Profile for buildRequest tests so we don't
// couple to the live profile registry (which can grow / mutate without
// invalidating these tests).
func stubProfile(prompt, system string) Profile {
	return Profile{
		Name:              "test",
		Prompt:            prompt,
		SystemInstruction: system,
		Schema:            &genai.Schema{Type: genai.TypeObject},
	}
}

func TestBuildRequest_NoSystemNoIntent(t *testing.T) {
	prof := stubProfile("describe this image", "")
	tb := int32(128)
	cfg, contents := buildRequest(prof, Options{}, []byte("jpegbytes"), &tb)
	if cfg.SystemInstruction != nil {
		t.Errorf("SystemInstruction should be nil when neither prof.SystemInstruction nor opts.Intent is set; got %+v", cfg.SystemInstruction)
	}
	if len(contents) != 1 || contents[0].Role != "user" {
		t.Errorf("expected single user content; got %+v", contents)
	}
}

func TestBuildRequest_OnlyProfileSystem(t *testing.T) {
	prof := stubProfile("describe", "you are a chart-extraction expert")
	tb := int32(128)
	cfg, _ := buildRequest(prof, Options{}, []byte("jpeg"), &tb)
	if cfg.SystemInstruction == nil {
		t.Fatal("SystemInstruction nil; expected profile text")
	}
	if cfg.SystemInstruction.Role != "" {
		t.Errorf("SystemInstruction.Role=%q; expected empty (Gemini's system slot does not use Role)", cfg.SystemInstruction.Role)
	}
	if len(cfg.SystemInstruction.Parts) != 1 {
		t.Fatalf("expected 1 part in SystemInstruction; got %d", len(cfg.SystemInstruction.Parts))
	}
	if got := cfg.SystemInstruction.Parts[0].Text; got != "you are a chart-extraction expert" {
		t.Errorf("system text = %q; want exact profile text", got)
	}
}

func TestBuildRequest_OnlyIntent(t *testing.T) {
	prof := stubProfile("describe", "")
	tb := int32(128)
	cfg, _ := buildRequest(prof, Options{Intent: "find the submit button"}, []byte("jpeg"), &tb)
	if cfg.SystemInstruction == nil {
		t.Fatal("SystemInstruction nil; expected intent-derived text")
	}
	got := cfg.SystemInstruction.Parts[0].Text
	if !strings.Contains(got, "find the submit button") {
		t.Errorf("system text %q should contain the intent; missing", got)
	}
	if strings.Contains(got, "\n\n") {
		t.Errorf("system text %q should not have leading composition separator when only intent is present", got)
	}
	// An explicit intent must carry the gating-override clause so a
	// profile (e.g. screenshot) doesn't decline a film still when the
	// task is "study this UI". See docs/feedback friction #2.
	if !strings.Contains(got, "overrides") {
		t.Errorf("intent text %q should tell the model an explicit task overrides type-based refusal", got)
	}
}

func TestBuildRequest_BothComposed(t *testing.T) {
	prof := stubProfile("describe", "you are a chart-extraction expert")
	tb := int32(128)
	cfg, _ := buildRequest(prof, Options{Intent: "find values for Q3"}, []byte("jpeg"), &tb)
	got := cfg.SystemInstruction.Parts[0].Text
	if !strings.HasPrefix(got, "you are a chart-extraction expert") {
		t.Errorf("composed text should begin with profile system; got %q", got)
	}
	if !strings.Contains(got, "\n\nThe downstream agent's specific task: find values for Q3") {
		t.Errorf("composed text should separate profile + intent with blank line; got %q", got)
	}
}

func TestBuildRequest_TemperatureDefaultsToZero(t *testing.T) {
	prof := stubProfile("p", "s")
	tb := int32(128)
	cfg, _ := buildRequest(prof, Options{}, []byte("jpeg"), &tb)
	if cfg.Temperature == nil {
		t.Fatal("Temperature pointer is nil; default should be a pointer to 0 (not omitted)")
	}
	if *cfg.Temperature != 0 {
		t.Errorf("default Temperature=%f; want 0 (schema-constrained extraction is deterministic-by-default)", *cfg.Temperature)
	}
}

func TestBuildRequest_TemperaturePassthrough(t *testing.T) {
	prof := stubProfile("p", "s")
	tb := int32(128)
	want := float32(0.7)
	cfg, _ := buildRequest(prof, Options{Temperature: &want}, []byte("jpeg"), &tb)
	if cfg.Temperature == nil || *cfg.Temperature != want {
		t.Errorf("Temperature=%v; want pointer to %f", cfg.Temperature, want)
	}
}

func TestBuildRequest_PartOrderIsTextFirst(t *testing.T) {
	prof := stubProfile("describe this image", "")
	tb := int32(128)
	_, contents := buildRequest(prof, Options{}, []byte("jpegbytes"), &tb)
	parts := contents[0].Parts
	if len(parts) != 2 {
		t.Fatalf("expected exactly 2 parts (text + image); got %d", len(parts))
	}
	if parts[0].Text == "" {
		t.Errorf("parts[0] should be the text prompt; got %+v", parts[0])
	}
	if parts[0].InlineData != nil {
		t.Errorf("parts[0] should NOT have InlineData (image must come second); got %+v", parts[0].InlineData)
	}
	if parts[1].InlineData == nil {
		t.Errorf("parts[1] should be InlineData (image after text); got %+v", parts[1])
	}
	if parts[1].Text != "" {
		t.Errorf("parts[1] should NOT have text; got %q", parts[1].Text)
	}
	if mt := parts[1].InlineData.MIMEType; mt != "image/jpeg" {
		t.Errorf("InlineData MIMEType=%q; want image/jpeg", mt)
	}
}

func TestBuildRequest_PreservesProfileSchema(t *testing.T) {
	prof := stubProfile("p", "s")
	tb := int32(128)
	cfg, _ := buildRequest(prof, Options{}, []byte("jpeg"), &tb)
	if cfg.ResponseSchema != prof.Schema {
		t.Errorf("ResponseSchema not preserved from profile")
	}
	if cfg.ResponseMIMEType != "application/json" {
		t.Errorf("ResponseMIMEType=%q; want application/json", cfg.ResponseMIMEType)
	}
}

func TestBuildRequest_ThinkingBudgetPassthrough(t *testing.T) {
	prof := stubProfile("p", "s")
	tb := int32(512)
	cfg, _ := buildRequest(prof, Options{}, []byte("jpeg"), &tb)
	if cfg.ThinkingConfig == nil || cfg.ThinkingConfig.ThinkingBudget == nil {
		t.Fatal("ThinkingConfig.ThinkingBudget nil")
	}
	if got := *cfg.ThinkingConfig.ThinkingBudget; got != 512 {
		t.Errorf("ThinkingBudget=%d; want 512", got)
	}
}

func TestDefaultThinkingBudgetFor_Flash(t *testing.T) {
	cases := []string{
		"gemini-2.5-flash",
		"gemini-2.5-flash-lite",
		"gemini-2.0-flash",
		"GEMINI-FLASH-something", // case-insensitive
	}
	for _, model := range cases {
		got := DefaultThinkingBudgetFor(model)
		if got == nil || *got != 0 {
			t.Errorf("DefaultThinkingBudgetFor(%q)=%v; want pointer to 0", model, got)
		}
	}
}

func TestDefaultThinkingBudgetFor_ProAndUnknown(t *testing.T) {
	cases := []string{
		"gemini-2.5-pro",
		"gemini-pro-latest",
		"gemini-3.1-pro-preview",
		"gemini-3-pro-image-preview",
		"unknown-future-model",
	}
	for _, model := range cases {
		got := DefaultThinkingBudgetFor(model)
		if got == nil || *got != MinProThinkingBudget {
			t.Errorf("DefaultThinkingBudgetFor(%q)=%v; want pointer to %d (defensive default for pro/unknown)", model, got, MinProThinkingBudget)
		}
	}
}

func TestDefaultThinkingBudgetFor_ReturnsFreshPointer(t *testing.T) {
	a := DefaultThinkingBudgetFor("gemini-2.5-pro")
	b := DefaultThinkingBudgetFor("gemini-2.5-pro")
	if a == b {
		t.Error("DefaultThinkingBudgetFor returned aliased pointer; expected fresh pointer each call so callers can mutate without poisoning")
	}
	*a = 999
	if *b != MinProThinkingBudget {
		t.Errorf("mutating one return value affected another: b=%d; want %d", *b, MinProThinkingBudget)
	}
}
