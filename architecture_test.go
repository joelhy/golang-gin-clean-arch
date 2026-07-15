package cleanarchgin_test

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestDomainPackagesDoNotImportAdapters(t *testing.T) {
	for _, pkg := range []string{"./user", "./product", "./order", "./reporting"} {
		cmd := exec.CommandContext(t.Context(), "go", "list", "-deps", "-f", "{{if not .Standard}}{{.ImportPath}}{{end}}", pkg)
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("go list %s: %v", pkg, err)
		}
		for _, forbidden := range []string{"gin-gonic", "gorm.io", "go-sql-driver", "golang-jwt", "urfave/cli", "cobra", "viper"} {
			if bytes.Contains(out, []byte(forbidden)) {
				t.Errorf("%s imports forbidden adapter dependency %q via:\n%s", pkg, forbidden, strings.TrimSpace(string(out)))
			}
		}
	}
}

func TestLegacyArchitectureRemoved(t *testing.T) {
	_, err := os.Stat("internal")
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("legacy internal directory still exists: %v", err)
	}
}
