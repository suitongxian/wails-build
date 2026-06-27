package repository

import (
	"strings"
	"testing"
)

func TestSubject_CRUD(t *testing.T) {
	db := openTestDB(t)
	repo := NewSubjectRepository(db)

	// Create
	contact := "13800000000"
	parentID := int64(0)
	created, err := repo.Create(CreateSubjectInput{
		Code:     "U-ZS",
		Name:     "张三",
		Type:     "person",
		ParentID: nil,
		Contact:  &contact,
	})
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
	if created.ID == 0 {
		t.Fatal("expected non-zero id")
	}
	if created.Status != "active" {
		t.Fatalf("default status should be active, got %s", created.Status)
	}

	// FindByID
	got, err := repo.FindByID(created.ID)
	if err != nil {
		t.Fatalf("findById failed: %v", err)
	}
	if got.Code != "U-ZS" || got.Name != "张三" {
		t.Fatalf("wrong subject: %+v", got)
	}

	// FindByCode
	byCode, err := repo.FindByCode("U-ZS")
	if err != nil {
		t.Fatalf("findByCode failed: %v", err)
	}
	if byCode.ID != created.ID {
		t.Fatalf("expected same id, got %d vs %d", byCode.ID, created.ID)
	}

	// Update
	newName := "张三丰"
	updated, err := repo.Update(created.ID, UpdateSubjectInput{Name: &newName})
	if err != nil {
		t.Fatalf("update failed: %v", err)
	}
	if updated.Name != "张三丰" {
		t.Fatalf("update name failed: %s", updated.Name)
	}

	// 添加部门
	if _, err := repo.Create(CreateSubjectInput{Code: "D-EDIT", Name: "编辑部", Type: "department"}); err != nil {
		t.Fatalf("create department failed: %v", err)
	}
	if _, err := repo.Create(CreateSubjectInput{Code: "D-PUB", Name: "出版部", Type: "department"}); err != nil {
		t.Fatalf("create department failed: %v", err)
	}

	// List 不过滤
	all, err := repo.List("", "")
	if err != nil {
		t.Fatalf("list all failed: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("expected 3, got %d", len(all))
	}

	// List 按 type 过滤
	persons, err := repo.List("person", "")
	if err != nil {
		t.Fatalf("list persons failed: %v", err)
	}
	if len(persons) != 1 {
		t.Fatalf("expected 1 person, got %d", len(persons))
	}

	// List 按 keyword 过滤
	depts, err := repo.List("", "部")
	if err != nil {
		t.Fatalf("list by keyword failed: %v", err)
	}
	if len(depts) != 2 {
		t.Fatalf("expected 2 dept matches, got %d", len(depts))
	}

	// SoftDelete
	if err := repo.SoftDelete(created.ID); err != nil {
		t.Fatalf("delete failed: %v", err)
	}
	if _, err := repo.FindByID(created.ID); err == nil {
		t.Fatal("expected not found after delete")
	}

	// 防止 unused
	_ = parentID
	_ = strings.Title
}

func TestSubject_DuplicateCode(t *testing.T) {
	db := openTestDB(t)
	repo := NewSubjectRepository(db)
	if _, err := repo.Create(CreateSubjectInput{Code: "X", Name: "x", Type: "person"}); err != nil {
		t.Fatalf("first create: %v", err)
	}
	if _, err := repo.Create(CreateSubjectInput{Code: "X", Name: "y", Type: "person"}); err == nil {
		t.Fatal("expected unique constraint to trigger")
	}
}

func TestSubject_ListHidesSystemSubjectsUnlessIncluded(t *testing.T) {
	db := openTestDB(t)
	repo := NewSubjectRepository(db)
	if _, err := repo.Create(CreateSubjectInput{Code: "SYS-PERSONAL-USER", Name: "本人", Type: "person"}); err != nil {
		t.Fatalf("create system subject: %v", err)
	}
	if _, err := repo.Create(CreateSubjectInput{Code: "DEPT-A", Name: "业务部门", Type: "department"}); err != nil {
		t.Fatalf("create business subject: %v", err)
	}

	visible, err := repo.List("", "")
	if err != nil {
		t.Fatalf("list visible subjects: %v", err)
	}
	if len(visible) != 1 || visible[0].Code != "DEPT-A" {
		t.Fatalf("default list should hide system subjects, got %+v", visible)
	}

	all, err := repo.ListWithOptions("", "", true)
	if err != nil {
		t.Fatalf("list with system subjects: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("includeSystem should return both subjects, got %+v", all)
	}
}
