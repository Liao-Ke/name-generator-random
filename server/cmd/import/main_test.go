// Package main 测试: 候选数据目录解析.
// 守栏目标: 无论从哪层目录执行 cmd/import, 都能定位到仓库根的候选数据目录,
// 用户不必显式设置 CANDIDATE_DATA_DIR (与 README 导入步骤一致).
//
// 注意: 测试不依赖进程 cwd —— 仓库根由源码文件位置推导, 避免 tester 改 cwd 后相互污染.
package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// repoRootFromSource 由本文件位置 (server/cmd/import/main_test.go) 反推仓库根.
func repoRootFromSource(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("无法定位测试源文件")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", ".."))
}

// TestResolveDataDirFrom_AnyStartDir 从多个起点解析默认值, 结果必须一致且真实存在.
// 起点覆盖: 包目录(最深) / server 模块根 / 仓库根 / 仓库内更深目录.
func TestResolveDataDirFrom_AnyStartDir(t *testing.T) {
	repoRoot := repoRootFromSource(t)
	want := filepath.Join(repoRoot, "api", "database", "candidate")

	starts := []string{
		filepath.Join(repoRoot, "server", "cmd", "import"),
		filepath.Join(repoRoot, "server"),
		filepath.Join(repoRoot, "server", "internal", "api"),
	}
	// NOTE 不含"仓库根"起点: DefaultDataDir 语义是"相对 server 模块根向上一级",
	// 从仓库根出发不含 server/ 前缀, 该起点无解 —— 从仓库根执行应走 `cd server` 或设 CANDIDATE_DATA_DIR.
	for _, base := range starts {
		t.Run(filepath.Base(base), func(t *testing.T) {
			got := resolveDataDirFrom(base, DefaultDataDir)
			if got != want {
				t.Errorf("从 %s 解析 = %q, 期望 %q", base, got, want)
			}
			if _, err := os.Stat(filepath.Join(got, markerFile)); err != nil {
				t.Errorf("解析结果缺少候选字库文件: %v", err)
			}
		})
	}
}

// TestResolveDataDirFrom_SourceRelativeDefault 默认值必须是"相对模块根向上一级",
// 即从 server 目录出发恰好命中仓库根下的数据目录. 改默认值时本测试会失败.
func TestResolveDataDirFrom_SourceRelativeDefault(t *testing.T) {
	repoRoot := repoRootFromSource(t)
	serverDir := filepath.Join(repoRoot, "server")
	want := filepath.Join(repoRoot, "api", "database", "candidate")
	if got := resolveDataDirFrom(serverDir, DefaultDataDir); got != want {
		t.Errorf("server 目录下解析 = %q, 期望 %q (DefaultDataDir=%q)", got, want, DefaultDataDir)
	}
}

// TestResolveDataDirFrom_AbsolutePathUnchanged 绝对路径不做查找, 原样返回.
// 生产部署把数据挂在容器内绝对路径, 这条路径不能被改写.
func TestResolveDataDirFrom_AbsolutePathUnchanged(t *testing.T) {
	abs := filepath.Join(t.TempDir(), "candidate")
	if got := resolveDataDirFrom(t.TempDir(), abs); got != abs {
		t.Errorf("绝对路径 = %q, 期望原样返回 %q", got, abs)
	}
}

// TestResolveDataDirFrom_NotFound 找不到时回退为绝对路径 (不 panic, 不返回相对路径).
func TestResolveDataDirFrom_NotFound(t *testing.T) {
	got := resolveDataDirFrom(t.TempDir(), "nonexistent/data/dir")
	if !filepath.IsAbs(got) {
		t.Errorf("未命中时应返回绝对路径, 实际 %q", got)
	}
}
