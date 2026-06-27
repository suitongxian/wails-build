package scanner

import "testing"

func TestIsSuspectNonPersonal(t *testing.T) {
	cases := []struct {
		name string
		path string
		size int64
		want bool
		why  string
	}{
		// === 路径模式：系统/缓存目录 ===
		{"macOS Library", "/Users/alice/Library/Caches/com.app/cache.db", 5000, true, "Library 系统目录"},
		{"macOS System", "/System/Library/CoreServices/Finder.app/Contents/Info.plist", 1500, true, "System"},
		{"Windows AppData", "C:\\Users\\bob\\AppData\\Local\\Temp\\foo.txt", 200, true, "AppData"},
		{"Windows Program Files", "C:\\Program Files\\Adobe\\config.xml", 5000, true, "Program Files"},
		{"Windows Recycle Bin", "C:\\$Recycle.Bin\\S-1-5\\file", 500, true, "$Recycle.Bin"},
		// === Linux 系统目录（两级路径）===
		{"Linux etc", "/etc/nginx/nginx.conf", 1500, true, "/etc/"},
		{"Linux etc multilevel", "/etc/systemd/system/foo.service", 2000, true, "/etc/"},
		{"Linux usr/lib", "/usr/lib/python3.10/something.py", 5000, true, "/usr/lib/"},
		{"Linux usr/share", "/usr/share/applications/firefox.desktop", 1000, true, "/usr/share/"},
		{"Linux usr/local/lib", "/usr/local/lib/node_modules/npm/package.json", 5000, true, "/usr/local/lib/"},
		{"Linux var/cache", "/var/cache/apt/archives/curl.deb", 99999, true, "/var/cache/"},
		{"Linux var/log", "/var/log/syslog.1", 500000, true, "/var/log/"},
		{"Linux var/lib", "/var/lib/postgresql/15/main/pg_wal/000.wal", 16777216, true, "/var/lib/"},
		{"Linux var/tmp", "/var/tmp/upload-staging.bin", 100000, true, "/var/tmp/"},
		{"Linux var/spool", "/var/spool/mail/root", 5000, true, "/var/spool/"},
		{"Linux snap", "/snap/firefox/3000/usr/lib/firefox/firefox", 999999, true, "/snap/"},
		{"Linux lost+found", "/home/user/disk/lost+found/inode-7", 99999, true, "/lost+found/"},
		{"Linux boot", "/boot/vmlinuz-5.15.0", 50000000, true, "/boot/"},

		// === Linux 反例：用户路径不应该被误标 ===
		{"Linux home dev username", "/home/dev/Documents/合同.pdf", 500000, false, "/home/dev/ 撞 dev 用户名风险，不收 /dev/"},
		{"Linux project named proc", "/home/alice/code/proj/proc/api.txt", 10000, false, "proj 子目录叫 proc 不该被标"},
		{"Linux project named sys", "/home/alice/code/proj/sys/util.py", 10000, false, "proj 子目录叫 sys 不该被标"},
		{"Linux user /usr-named path", "/home/usr/notes.txt", 1000, false, "用户名叫 usr 也不该被误标"},
		{"Linux project named var", "/home/alice/code/var/state.json", 5000, false, "项目子目录叫 var 不该被标"},
		{"Linux project named tmp", "/home/alice/proj/tmp/draft.md", 1000, false, "项目下临时草稿不该被标"},
		{"git internal", "/home/dev/proj/.git/objects/aa/abc.pack", 99999, true, ".git/"},
		{"node_modules", "/home/dev/web/node_modules/react/index.js", 5000, true, "node_modules"},
		{"__pycache__", "/home/dev/p/__pycache__/foo.cpython-310.pyc", 800, true, "__pycache__"},
		{"hidden cache", "/home/dev/.cache/pip/wheels/x.whl", 9999, true, ".cache"},
		{"IDE config", "/home/dev/proj/.idea/workspace.xml", 5000, true, ".idea"},

		// === 后缀模式：明显二进制/构建产物 ===
		{"DLL", "/some/regular/path/lib.dll", 50000, true, ".dll"},
		{"EXE", "C:\\Users\\alice\\Downloads\\setup.exe", 999999, true, ".exe"},
		{"ELF .so", "/home/dev/build/lib.so", 100000, true, ".so"},
		{"font ttf", "/home/dev/Documents/font.ttf", 80000, true, ".ttf"},
		{"font woff2", "/home/dev/web/font.woff2", 50000, true, ".woff2"},
		{"DS_Store", "/Users/alice/Documents/.DS_Store", 6148, true, ".DS_Store"},
		{"pyc", "/home/dev/p/foo.pyc", 5000, true, ".pyc"},
		{"class file", "/home/dev/proj/build/Foo.class", 5000, true, ".class"},
		{"jar", "/home/dev/lib/spring.jar", 99999, true, ".jar"},
		{"log file", "/home/dev/Documents/server.log", 5000, true, ".log"},

		// === 复合启发式：小文件 + 非文档后缀 ===
		{"tiny obscure ext", "/home/dev/Documents/foo.xyz", 100, true, "tiny + non-doc ext"},
		{"tiny no ext", "/home/dev/Documents/Makefile", 500, true, "tiny + no ext (no doc match)"},

		// === 反例：正常个人文件 ===
		{"doc in home", "/home/dev/Documents/合同.pdf", 500000, false, "正常 PDF"},
		{"docx in workspace", "/home/dev/工作/汇报.docx", 80000, false, "正常 Office 文件"},
		{"jpg photo", "/home/dev/Pictures/holiday.jpg", 200000, false, "正常照片"},
		{"large jpg even tiny", "/home/dev/Pictures/x.jpg", 500, false, ".jpg 是 doc-like，不因小而 suspect"},
		{"small pdf", "/home/dev/Documents/notice.pdf", 500, false, "小 PDF 也是 doc，不 suspect"},
		{"tiny txt", "/home/dev/Documents/note.txt", 100, false, ".txt 是 doc-like，不因小而 suspect"},
		{"normal path normal ext", "/home/dev/Code/myproj/README.md", 2000, false, "正常代码仓库 README"},
		// 边界：路径不含 system 关键字，后缀不在黑名单，但 < 1KB 又是文档 → 不 suspect
		{"small csv", "/home/dev/Documents/budget.csv", 500, false, ".csv 是文档"},
	}

	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			got := IsSuspectNonPersonal(c.path, c.size)
			if got != c.want {
				t.Errorf("IsSuspectNonPersonal(%q, %d) = %v, want %v (%s)",
					c.path, c.size, got, c.want, c.why)
			}
		})
	}
}

// 路径大小写不敏感
func TestIsSuspectNonPersonal_CaseInsensitivePath(t *testing.T) {
	upperPath := "/Users/alice/LIBRARY/Caches/x.db"
	if !IsSuspectNonPersonal(upperPath, 1000) {
		t.Errorf("expected uppercase LIBRARY path to match")
	}
}

// Windows 反斜杠路径
func TestIsSuspectNonPersonal_BackslashPath(t *testing.T) {
	winPath := `C:\Windows\System32\drivers\etc\hosts`
	if !IsSuspectNonPersonal(winPath, 5000) {
		t.Errorf("expected Windows backslash path to match (windows + system)")
	}
}

// 后缀大小写不敏感
func TestIsSuspectNonPersonal_CaseInsensitiveExt(t *testing.T) {
	if !IsSuspectNonPersonal("/some/path/lib.DLL", 100000) {
		t.Errorf("uppercase .DLL should match")
	}
}
