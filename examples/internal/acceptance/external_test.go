package acceptance_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestExamplesCopyToExternalModule(t *testing.T) {
	t.Parallel()
	root, err := filepath.Abs("../../..")
	require.NoError(t, err)
	dir := t.TempDir()
	// replace 只用于编译当前候选源码；外部 module 名称使 internal 导入仍被 Go 拒绝。
	module := "module example.org/genesis-consumer\n\ngo 1.26\n\nrequire github.com/ceyewan/genesis v1.0.0\nreplace github.com/ceyewan/genesis => " + filepath.ToSlash(root) + "\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte(module), 0o644))
	// 文档直接引用可运行代码；外部复制验收编译同一份源码，不维护文档代码副本。
	for _, name := range []string{"mq", "dlock", "connector", "cached-query"} {
		data, err := os.ReadFile(filepath.Join(root, "examples", name, "main.go"))
		require.NoError(t, err)
		dest := filepath.Join(dir, name)
		require.NoError(t, os.MkdirAll(dest, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(dest, "main.go"), data, 0o644))
	}
	for _, args := range [][]string{{"mod", "tidy"}, {"test", "-run", "^$", "./..."}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			// race 缓存不能代替普通构建缓存，每个阶段独立预留冷构建预算。
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
			defer cancel()
			cmd := exec.CommandContext(ctx, "go", args...)
			cmd.Dir = dir
			cmd.Env = append(os.Environ(), "GOWORK=off")
			started := time.Now()
			out, err := cmd.CombinedOutput()
			t.Logf("go %s: elapsed=%s, context=%v", strings.Join(args, " "), time.Since(started), ctx.Err())
			require.NoError(t, err, "go %s: context=%v\n%s", strings.Join(args, " "), ctx.Err(), out)
		})
	}
}
