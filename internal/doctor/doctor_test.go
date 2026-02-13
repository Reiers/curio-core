package doctor

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRunPostImportChecks(t *testing.T) {
	d := t.TempDir()
	if err := os.MkdirAll(filepath.Join(d, "chainstore"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(d, "blockstore"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, "blockstore", "abc.blk"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	head := `{"height":1,"tipsetKey":"abc","stateRoot":"abc"}`
	if err := os.WriteFile(filepath.Join(d, "chainstore", "head.json"), []byte(head), 0o644); err != nil {
		t.Fatal(err)
	}
	checks, err := RunPostImport(d)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range checks {
		if !c.OK {
			t.Fatalf("expected check %s to pass: %s", c.Name, c.Message)
		}
	}
}
