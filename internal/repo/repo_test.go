package repo

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitLayout(t *testing.T) {
	dir := t.TempDir()
	work := filepath.Join(dir, "proj")
	gitDir, placeholder, err := Init(work)
	if err != nil {
		t.Fatal(err)
	}
	if !placeholder {
		t.Fatal("无环境变量时应使用占位 user")
	}
	for _, d := range []string{
		"objects", "objects/info", "objects/pack",
		"refs", "refs/heads", "refs/tags",
		"branches", "hooks", "info",
	} {
		st, err := os.Stat(filepath.Join(gitDir, d))
		if err != nil || !st.IsDir() {
			t.Fatalf("缺少目录 .git/%s", d)
		}
	}
	head, err := os.ReadFile(filepath.Join(gitDir, "HEAD"))
	if err != nil || string(head) != "ref: refs/heads/main\n" {
		t.Fatalf("HEAD 不对：%q %v", head, err)
	}
	cfg, err := os.ReadFile(filepath.Join(gitDir, "config"))
	if err != nil || !strings.Contains(string(cfg), "repositoryformatversion = 0") ||
		!strings.Contains(string(cfg), "name = Your Name") {
		t.Fatalf("config 不对：%q %v", cfg, err)
	}
	if _, err := os.Stat(filepath.Join(gitDir, "description")); err != nil {
		t.Fatal("缺少 description")
	}
	// 幂等：已有 config 不被覆盖。
	custom := "[user]\n\tname = Real Name\n"
	if err := os.WriteFile(filepath.Join(gitDir, "config"), []byte(custom), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Init(work); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(filepath.Join(gitDir, "config"))
	if string(after) != custom {
		t.Fatalf("重新 init 覆盖了已有的 config：%q", after)
	}
}

func TestInitWithEnvIdentity(t *testing.T) {
	t.Setenv("GIT_AUTHOR_NAME", "Env User")
	t.Setenv("GIT_AUTHOR_EMAIL", "env@example.com")
	dir := t.TempDir()
	gitDir, placeholder, err := Init(filepath.Join(dir, "proj"))
	if err != nil {
		t.Fatal(err)
	}
	if placeholder {
		t.Fatal("环境变量给了身份，不应再用占位值")
	}
	cfg, _ := os.ReadFile(filepath.Join(gitDir, "config"))
	if !strings.Contains(string(cfg), "name = Env User") || !strings.Contains(string(cfg), "email = env@example.com") {
		t.Fatalf("config 未用环境变量身份：%q", cfg)
	}
}

func TestFindRepo(t *testing.T) {
	dir := t.TempDir()
	work := filepath.Join(dir, "proj")
	if _, err := os.Stat(work); os.IsNotExist(err) {
		// Init 会创建目录
	}
	gitDir, _, err := Init(work)
	if err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(work, "a", "b", "c")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	found, foundWork, err := FindRepo(sub)
	if err != nil || found != gitDir || foundWork != work {
		t.Fatalf("子目录向上找仓库失败：%s %s %v", found, foundWork, err)
	}
	// 仓库外报错。
	if _, _, err := FindRepo(dir); err == nil {
		t.Fatal("仓库外应报错")
	}
	// 直接在工作区根目录找。
	if _, _, err := FindRepo(work); err != nil {
		t.Fatalf("工作区根目录应能找到仓库：%v", err)
	}
}
