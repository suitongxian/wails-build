package repository

import "testing"

// V3-4 §7.7 文档列出 9 个权限动作，常量必须全部对齐
func TestAllPermActions_Length(t *testing.T) {
	got := AllPermActions()
	if len(got) != 9 {
		t.Errorf("文档 §7.7 列 9 个权限动作，常量返回 %d 个", len(got))
	}
}

// V3-4 必须含 upload 与 destroy（V1 缺这俩常量）
func TestAllPermActions_IncludesUploadAndDestroy(t *testing.T) {
	all := AllPermActions()
	wantContains := []string{"read", "write", "receive", "upload", "submit", "share", "archive", "close", "destroy"}
	for _, w := range wantContains {
		found := false
		for _, a := range all {
			if a == w {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("权限常量缺少 %q（文档 §7.7 列出）", w)
		}
	}
}

// V3-4 IsValidPermAction 拒绝未定义动作
func TestIsValidPermAction(t *testing.T) {
	for _, a := range AllPermActions() {
		if !IsValidPermAction(a) {
			t.Errorf("%s 应当合法", a)
		}
	}
	for _, bad := range []string{"", "delete", "create", "modify", "execute"} {
		if IsValidPermAction(bad) {
			t.Errorf("%q 不应合法", bad)
		}
	}
}

// V3-4 常量值字面量与文档一致
func TestPermConstantValues(t *testing.T) {
	if PermRead != "read" || PermWrite != "write" || PermReceive != "receive" {
		t.Error("PermRead/Write/Receive 字面量错")
	}
	if PermUpload != "upload" || PermSubmit != "submit" || PermShare != "share" {
		t.Error("PermUpload/Submit/Share 字面量错")
	}
	if PermArchive != "archive" || PermClose != "close" || PermDestroy != "destroy" {
		t.Error("PermArchive/Close/Destroy 字面量错")
	}
}
