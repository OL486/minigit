// Identity 从 .git/config 读提交身份。
//
// 环境变量优先于配置文件（和真 git 一致）：作者看 GIT_AUTHOR_NAME /
// GIT_AUTHOR_EMAIL，提交者看 GIT_COMMITTER_NAME / GIT_COMMITTER_EMAIL；
// 环境变量没给全才读 config 的 [user] 节。都没有则报错，不猜。
package repo

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Identity 是一组名字 + 邮箱。
type Identity struct {
	Name  string
	Email string
}

// AuthorIdentity 返回作者身份。
func AuthorIdentity(gitDir string) (Identity, error) {
	return identityFrom(gitDir, "GIT_AUTHOR_NAME", "GIT_AUTHOR_EMAIL")
}

// CommitterIdentity 返回提交者身份。
func CommitterIdentity(gitDir string) (Identity, error) {
	return identityFrom(gitDir, "GIT_COMMITTER_NAME", "GIT_COMMITTER_EMAIL")
}

func identityFrom(gitDir, nameEnv, emailEnv string) (Identity, error) {
	name, email := os.Getenv(nameEnv), os.Getenv(emailEnv)
	if name != "" && email != "" {
		return Identity{Name: name, Email: email}, nil
	}
	// 环境变量没给全：读 config 的 [user] 节。
	data, err := os.ReadFile(filepath.Join(gitDir, "config"))
	if err != nil {
		return Identity{}, fmt.Errorf("读取 .git/config 失败：%w", err)
	}
	cfgName, cfgEmail := "", ""
	section := ""
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.ToLower(strings.TrimSpace(line[1 : len(line)-1]))
			// "[user]" 与 "[user \"x\"]" 都按 user 处理；M2 不用子节。
			if i := strings.Index(section, " "); i >= 0 {
				section = section[:i]
			}
			continue
		}
		if section != "user" {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(strings.ToLower(key))
		val = strings.TrimSpace(val)
		val = strings.Trim(val, `"`) // 去掉可能的外层引号
		switch key {
		case "name":
			cfgName = val
		case "email":
			cfgEmail = val
		}
	}
	if name == "" {
		name = cfgName
	}
	if email == "" {
		email = cfgEmail
	}
	if name == "" || email == "" {
		return Identity{}, fmt.Errorf("未设置 user.name / user.email：请编辑 .git/config 或设置 %s / %s", nameEnv, emailEnv)
	}
	return Identity{Name: name, Email: email}, nil
}
