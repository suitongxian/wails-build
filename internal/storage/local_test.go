package storage

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// V3-6 §7.8 接口实现完整性（编译期已断言 _ Adapter = ...）
func TestLocalAdapter_ImplementsAdapter(t *testing.T) {
	var _ Adapter = NewLocalFileStorageAdapter(t.TempDir())
}

// V3-6 §7.8 CreateProjectDirectory：项目目录 + metadata + archive + stages 三态全建
func TestLocalAdapter_CreateProjectDirectory(t *testing.T) {
	root := t.TempDir()
	a := NewLocalFileStorageAdapter(root)
	err := a.CreateProjectDirectory("P-001", []string{"S1", "S2"})
	if err != nil {
		t.Fatal(err)
	}
	wantDirs := []string{
		"P-001/metadata", "P-001/archive",
		"P-001/stages/S1/input", "P-001/stages/S1/process", "P-001/stages/S1/output",
		"P-001/stages/S2/input", "P-001/stages/S2/process", "P-001/stages/S2/output",
	}
	for _, d := range wantDirs {
		path := filepath.Join(root, d)
		if info, err := os.Stat(path); err != nil || !info.IsDir() {
			t.Errorf("目录 %s 应当存在", d)
		}
	}
}

// V3-6 §7.8 CreateStageDirectory 单独补 stage 目录
func TestLocalAdapter_CreateStageDirectory(t *testing.T) {
	root := t.TempDir()
	a := NewLocalFileStorageAdapter(root)
	a.CreateProjectDirectory("P-002", []string{"S1"})
	if err := a.CreateStageDirectory("P-002", "S2"); err != nil {
		t.Fatal(err)
	}
	for _, st := range []string{"input", "process", "output"} {
		path := filepath.Join(root, "P-002", "stages", "S2", st)
		if info, err := os.Stat(path); err != nil || !info.IsDir() {
			t.Errorf("S2/%s 应建", st)
		}
	}
}

// V3-6 §7.8 SaveFile 流式写盘 + checksum
func TestLocalAdapter_SaveFile(t *testing.T) {
	root := t.TempDir()
	a := NewLocalFileStorageAdapter(root)
	res, err := a.SaveFile(SaveFileInput{
		Reader:         strings.NewReader("hello world"),
		ProjectCode:    "P-003",
		StageCode:      "S1",
		DataState:      "output",
		TargetFileName: "f.txt",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Size != 11 {
		t.Errorf("size 应为 11, got %d", res.Size)
	}
	// "hello world" SHA-256 uppercase
	want := "B94D27B9934D3E08A52E52D7DA7DABFAC484EFE37A5380EE9088F7ACE2EFCDE9"
	if res.Checksum != want {
		t.Errorf("checksum 错: got %s want %s", res.Checksum, want)
	}
	// 物理文件存在
	if _, err := os.Stat(res.StorageURI); err != nil {
		t.Errorf("file missing: %v", err)
	}
}

// V3-6 §7.8 CalculateChecksum 复算等于 SaveFile 内置 checksum
func TestLocalAdapter_CalculateChecksum(t *testing.T) {
	root := t.TempDir()
	a := NewLocalFileStorageAdapter(root)
	res, _ := a.SaveFile(SaveFileInput{
		Reader: bytes.NewBufferString("X"), ProjectCode: "P", StageCode: "S", DataState: "input", TargetFileName: "x.bin",
	})
	size, sum, err := a.CalculateChecksum(res.StorageURI)
	if err != nil {
		t.Fatal(err)
	}
	if size != res.Size || sum != res.Checksum {
		t.Errorf("复算 mismatch")
	}
}

// V3-6 §7.8 MoveFile：同盘内 rename
func TestLocalAdapter_MoveFile(t *testing.T) {
	root := t.TempDir()
	a := NewLocalFileStorageAdapter(root)
	src, _ := a.SaveFile(SaveFileInput{
		Reader: strings.NewReader("DATA"), ProjectCode: "P", StageCode: "S1", DataState: "input", TargetFileName: "a.txt",
	})
	newURI, err := a.MoveFile(src.StorageURI, "P", "S2", "process", "a.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(src.StorageURI); !os.IsNotExist(err) {
		t.Errorf("旧位置应已不存在")
	}
	if _, err := os.Stat(newURI); err != nil {
		t.Errorf("新位置应有文件: %v", err)
	}
}

// V3-6 §7.8 CopyAsInput：下游拿到独立副本，源文件不动
func TestLocalAdapter_CopyAsInput(t *testing.T) {
	root := t.TempDir()
	a := NewLocalFileStorageAdapter(root)
	src, _ := a.SaveFile(SaveFileInput{
		Reader: strings.NewReader("UPSTREAM"), ProjectCode: "P", StageCode: "S1", DataState: "output", TargetFileName: "out.txt",
	})
	newURI, size, checksum, err := a.CopyAsInput(src.StorageURI, "P", "S2", "in.txt")
	if err != nil {
		t.Fatal(err)
	}
	if size != src.Size || checksum != src.Checksum {
		t.Errorf("副本元数据应与源一致")
	}
	// 源仍在
	if _, err := os.Stat(src.StorageURI); err != nil {
		t.Errorf("§10.3 输入文件默认不可覆盖来源——源必须保留: %v", err)
	}
	// 副本独立存在
	if _, err := os.Stat(newURI); err != nil {
		t.Errorf("副本应当存在")
	}
}

// V3-6 §7.8 SealArchive 写 manifest.json + 计算 sha256
func TestLocalAdapter_SealArchive(t *testing.T) {
	root := t.TempDir()
	a := NewLocalFileStorageAdapter(root)
	a.CreateProjectDirectory("P", nil)
	path, sum, err := a.SealArchive("P", `{"foo":"bar"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(path, "manifest.json") {
		t.Errorf("路径应以 manifest.json 结尾")
	}
	if sum == "" || len(sum) != 64 {
		t.Errorf("sha256 应为 64 字符 uppercase hex")
	}
	body, _ := os.ReadFile(path)
	if string(body) != `{"foo":"bar"}` {
		t.Errorf("manifest 内容错")
	}
}

// V3-6 §7.8 + §17.6 DeleteFile：删除文件，幂等
func TestLocalAdapter_DeleteFile(t *testing.T) {
	root := t.TempDir()
	a := NewLocalFileStorageAdapter(root)
	res, _ := a.SaveFile(SaveFileInput{
		Reader: strings.NewReader("X"), ProjectCode: "P", StageCode: "S", DataState: "input", TargetFileName: "x.txt",
	})
	if err := a.DeleteFile(res.StorageURI); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(res.StorageURI); !os.IsNotExist(err) {
		t.Errorf("文件应已删除")
	}
	// 幂等：再删一次不报错
	if err := a.DeleteFile(res.StorageURI); err != nil {
		t.Errorf("删除不存在文件应幂等: %v", err)
	}
	// 空 uri 拒
	if err := a.DeleteFile(""); err == nil {
		t.Error("空 uri 应报错")
	}
}
