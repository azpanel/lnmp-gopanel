package lnmpinstall

import (
	"errors"
	"regexp"
	"strconv"
	"strings"
)

// ParseOptions reads only the known literal selection protocol. It never sources
// downloaded shell code to discover the form. A changed protocol fails closed.
func ParseOptions(main, install, security, revision, arch, ip string) (Options, error) {
	o := Options{Revision: revision, Architecture: arch, IPv4: ip, Modules: Modules}
	errProtocol := errors.New("上游安装交互已变化，当前面板暂不支持，请更新面板适配")
	selection := regexp.MustCompile(`(?s)Dispaly_Selection\(\)\s*\{(.*?)\n\}`).FindStringSubmatch(main)
	if len(selection) != 2 || strings.Join(strings.Fields(selection[1]), " ") != "Database_Selection PHP_Selection MemoryAllocator_Selection Default_Security_Selection || exit 1" {
		return o, errProtocol
	}
	checkReads := func(source string, allowed map[string]bool) bool {
		readWord := regexp.MustCompile(`\bread\s`)
		last := regexp.MustCompile(`([a-zA-Z_][a-zA-Z0-9_]*)\s*$`)
		for _, line := range strings.Split(source, "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "#") || !readWord.MatchString(line) {
				continue
			}
			line, _, _ = strings.Cut(line, "||")
			m := last.FindStringSubmatch(line)
			if len(m) != 2 || !allowed[m[1]] {
				return false
			}
		}
		return true
	}
	mainSelection, _, _ := strings.Cut(main, "Apache_Selection()")
	if !checkReads(mainSelection, map[string]bool{"DBSelect": true, "Bin": true, "DB_Root_Password": true, "InstallInnodb": true, "PHPSelect": true, "SelectMalloc": true}) {
		return o, errProtocol
	}
	_, securitySelection, okSecurity := strings.Cut(security, "Default_Security_Selection()")
	if !okSecurity {
		return o, errProtocol
	}
	securitySelection, _, _ = strings.Cut(securitySelection, "Prepare_Default_Security()")
	if !checkReads(securitySelection, map[string]bool{"PhpMyAdmin_Mode": true, "PhpMyAdmin_Auth_User": true, "PhpMyAdmin_Auth_Password": true, "confirmation": true, "Default_Server_IP": true}) {
		return o, errProtocol
	}
	array := func(name string, count int) ([]Choice, bool) {
		m := regexp.MustCompile(`(?m)^` + name + `=\(([^\n]*)\)\s*$`).FindStringSubmatch(main)
		if len(m) != 2 {
			return nil, false
		}
		re := regexp.MustCompile(`'([^'\r\n]+)'`)
		values := re.FindAllStringSubmatch(m[1], -1)
		if len(values) != count || strings.TrimSpace(re.ReplaceAllString(m[1], "")) != "" {
			return nil, false
		}
		out := []Choice{}
		for i, v := range values {
			out = append(out, Choice{i + 1, v[1]})
		}
		return out, true
	}
	var ok bool
	if o.Databases, ok = array("DB_Info", 13); !ok {
		return o, errProtocol
	}
	if o.PHP, ok = array("PHP_Info", 16); !ok {
		return o, errProtocol
	}
	o.Databases = append([]Choice{{0, "不安装数据库"}}, o.Databases...)
	defaultValue := func(name string) int {
		m := regexp.MustCompile(`(?m)^\s*` + name + `="([0-9]+)"`).FindStringSubmatch(main)
		if len(m) != 2 {
			return -1
		}
		n, _ := strconv.Atoi(m[1])
		return n
	}
	o.DefaultDatabase, o.DefaultPHP, o.DefaultAllocator = defaultValue("DBSelect"), defaultValue("PHPSelect"), defaultValue("SelectMalloc")
	if o.DefaultDatabase < 0 || o.DefaultDatabase > 13 || o.DefaultPHP < 1 || o.DefaultPHP > 16 || o.DefaultAllocator < 1 || o.DefaultAllocator > 3 {
		return o, errProtocol
	}
	for _, marker := range []string{"Database_Selection", "PHP_Selection", "MemoryAllocator_Selection", "Default_Security_Selection", "LNMP_Auto", "DB_Root_Password", "InstallInnodb", "SelectMalloc"} {
		if !strings.Contains(main, marker) {
			return o, errProtocol
		}
	}
	if !strings.Contains(install, "Dispaly_Selection") || !strings.Contains(install, "LNMP_Stack") || !strings.Contains(security, "PhpMyAdmin_Auth_Password") || !strings.Contains(security, "Default_Server_IP") || !revisionPattern.MatchString(revision) {
		return o, errProtocol
	}
	return o, nil
}
