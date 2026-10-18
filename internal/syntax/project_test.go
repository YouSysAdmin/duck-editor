package syntax

import (
	"testing"
)

// The project .duck/languages.yaml must parse and turn writestring off.
func TestProjectLanguagesOverride(t *testing.T) {
	t.Parallel()

	langs, err := LoadLanguages("../../.duck/languages.yaml")
	if err != nil {
		t.Fatal(err)
	}

	srv, ok := langs.Server("gopls")
	if !ok {
		t.Fatal("gopls missing")
	}

	cfg, _ := srv.Config.(map[string]any)
	analyses, _ := cfg["analyses"].(map[string]any)
	if analyses["writestring"] != false {
		t.Fatalf("gopls config = %v", srv.Config)
	}

	if hints, _ := cfg["hints"].(map[string]any); hints["parameterNames"] != true {
		t.Fatalf("hints lost: %v", cfg["hints"])
	}
}
