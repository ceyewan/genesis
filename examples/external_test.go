package examples_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExamplesCopyToExternalModule(t *testing.T) {
	t.Parallel()
	root, err := filepath.Abs("..")
	require.NoError(t, err)
	dir := t.TempDir()
	// replace 只用于编译当前候选源码；外部 module 名称使 internal 导入仍被 Go 拒绝。
	module := "module example.org/genesis-consumer\n\ngo 1.26\n\nrequire github.com/ceyewan/genesis v1.0.0-rc.3\nreplace github.com/ceyewan/genesis => " + filepath.ToSlash(root) + "\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte(module), 0o644))
	for _, name := range []string{"mq", "dlock"} {
		data, err := os.ReadFile(filepath.Join(name, "main.go"))
		require.NoError(t, err)
		dest := filepath.Join(dir, name)
		require.NoError(t, os.MkdirAll(dest, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(dest, "main.go"), data, 0o644))
	}
	for _, args := range [][]string{{"mod", "tidy"}, {"test", "-run", "^$", "./..."}} {
		cmd := exec.Command("go", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GOWORK=off")
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "%s", out)
	}
}
