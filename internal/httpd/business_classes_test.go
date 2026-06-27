package httpd

import (
	"testing"
)

// TestBusinessClassesHTTP 验证行业分类 CRUD 的 HTTP 闭环（片2）
func TestBusinessClassesHTTP(t *testing.T) {
	r, _, cleanup := setupTestServer(t)
	defer cleanup()

	// 创建
	code, resp := jsonReq(t, r, "POST", "/business-classes", map[string]any{
		"name":        "出版印刷",
		"description": "图书/期刊印刷数据业务",
	})
	if code != 200 || resp["success"] != true {
		t.Fatalf("创建失败: code=%d resp=%v", code, resp)
	}
	created := dataMap(t, resp)
	id := int64(created["id"].(float64))
	if id == 0 {
		t.Fatal("创建后应返回 id")
	}
	if created["code"] == "" || created["code"] == nil {
		t.Fatal("创建后应返回自动生成的 code")
	}

	// 列表含刚建的
	code, resp = jsonReqNoBody(t, r, "GET", "/business-classes")
	if code != 200 || resp["success"] != true {
		t.Fatalf("列表失败: code=%d resp=%v", code, resp)
	}
	list, ok := resp["data"].([]interface{})
	if !ok || len(list) == 0 {
		t.Fatalf("列表应非空: %v", resp["data"])
	}

	// 更新
	code, resp = jsonReq(t, r, "PUT", "/business-classes/"+itoa(id), map[string]any{
		"name":        "出版印刷业",
		"description": "改了",
	})
	if code != 200 || resp["success"] != true {
		t.Fatalf("更新失败: code=%d resp=%v", code, resp)
	}

	// 删除
	code, resp = jsonReqNoBody(t, r, "DELETE", "/business-classes/"+itoa(id))
	if code != 200 || resp["success"] != true {
		t.Fatalf("删除失败: code=%d resp=%v", code, resp)
	}

	// 删除后列表不再含它（删空时 data 可能为 null）
	_, resp = jsonReqNoBody(t, r, "GET", "/business-classes")
	if rows, ok := resp["data"].([]interface{}); ok {
		for _, x := range rows {
			m := x.(map[string]interface{})
			if int64(m["id"].(float64)) == id {
				t.Fatal("删除后列表不应再含该行业")
			}
		}
	}
}
