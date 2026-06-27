package repository

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFetchManageUsers(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/auth-users/list" {
			if r.URL.Query().Get("status") != "active" {
				t.Errorf("应只拉 active 用户，query=%s", r.URL.RawQuery)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":0,"message":"success","data":[
				{"username":"liu","display_name":"刘老师","user_unit":"第一研究院","user_department":"档案处","status":"active"},
				{"username":"wang","display_name":"王老师","user_unit":"第一研究院","user_department":"编辑部","status":"active"}
			]}`))
			return
		}
		w.WriteHeader(404)
	}))
	defer srv.Close()

	users, err := FetchManageUsers(srv.Client(), srv.URL)
	if err != nil {
		t.Fatalf("拉取失败: %v", err)
	}
	if len(users) != 2 {
		t.Fatalf("应拉到 2 个用户，实得 %d", len(users))
	}
	if users[0].DisplayName != "刘老师" || users[0].Username != "liu" {
		t.Fatalf("用户字段解析不符: %+v", users[0])
	}
}

func TestFetchManageUsers_NoEndpoint(t *testing.T) {
	if _, err := FetchManageUsers(nil, ""); err == nil {
		t.Fatal("未配置 endpoint 应报错")
	}
}

func TestFetchManageBusinessClasses(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/business-classes/list" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":[
				{"id":1,"code":"IND-001","name":"出版印刷","type":"industry","description":"图书/期刊"},
				{"id":2,"code":"IND-002","name":"政务","type":"industry","description":null}
			]}`))
			return
		}
		w.WriteHeader(404)
	}))
	defer srv.Close()

	classes, err := FetchManageBusinessClasses(srv.Client(), srv.URL)
	if err != nil {
		t.Fatalf("拉取失败: %v", err)
	}
	if len(classes) != 2 || classes[0].Code != "IND-001" || classes[0].Name != "出版印刷" {
		t.Fatalf("分类解析不符: %+v", classes)
	}
}
