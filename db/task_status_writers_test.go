package db

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// M1b.3a 钉住测试：tasks.status 的 SQL 写入面必须保持唯一——仅 db.SetStatus、
// db.SetTerminalStatusGuarded（db/tasks.go）与 ApplyTaskAdmission（server/manager.go）
// 三处。新增任何直接改 tasks.status 的语句都是完成门绕过面，必须先过
// M1b.3b 的共享事务性完成操作评审。纯源码扫描，不依赖数据库。
func TestTaskStatusWriteSurfaceIsPinned(t *testing.T) {
	pattern := regexp.MustCompile(`(?s)UPDATE\s+tasks\s+SET\s+status`)
	allowed := map[string]int{
		"db/tasks.go":       2, // SetStatus + SetTerminalStatusGuarded
		"server/manager.go": 1, // ApplyTaskAdmission (admission CAS, never writes done)
	}
	roots := []struct{ dir, prefix string }{{".", "db"}, {"../server", "server"}}
	found := map[string]int{}
	for _, r := range roots {
		_ = filepath.WalkDir(r.dir, func(path string, ent fs.DirEntry, err error) error {
			if err != nil || ent.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			raw, rerr := os.ReadFile(path)
			if rerr != nil {
				return nil
			}
			if n := len(pattern.FindAll(raw, -1)); n > 0 {
				found[r.prefix+"/"+filepath.Base(path)] += n
			}
			return nil
		})
	}
	if len(found) != len(allowed) {
		t.Fatalf("tasks.status write surface changed: found %v, allowed %v — new direct writers must go through the M1b.3b shared completion operation", found, allowed)
	}
	for k, want := range allowed {
		if found[k] != want {
			t.Fatalf("%s: %d status-writing statements, allowed %d", k, found[k], want)
		}
	}
}
