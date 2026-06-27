package repository

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/jmoiron/sqlx"
)

// 集中立项(CPA)跨机文件交接。
//
// 与传统多人协同(stage_finals_transfer.go)的关键区别：
//   - CPA 的本机项目目录名是「{项目名}-{编码}」(centralizedDirCode)，仅用于本机定位；
//   - manage 存储键必须用【项目级全局唯一】的 project_code(XM-YYYY-NNNN)，否则多个 CPA
//     项目共用同一业务模版编码时定稿会跨项目串档。
//
// 因此这里把「本机目录名(localProjectDir)」与「manage 键(projectKey)」拆成两个参数，
// 复用 uploadOneFinal/downloadOneFinal 但分别取值，避免直接复用 UploadStageFinals 的单参数歧义。

// collectCentralizedStageOutputs 汇总某环节全部文件任务的 output 定稿(任务级 + 兼容遗留环节级)，
// 跳过空/占位文件，按文件名去重。返回绝对路径列表。
func collectCentralizedStageOutputs(ws *ProjectWorkspace, localProjectDir, stageCode string) []string {
	dirs := []string{}
	tasks, _ := ws.ListTaskCodes(localProjectDir, stageCode)
	for _, tc := range tasks {
		dirs = append(dirs, ws.TaskStateDir(localProjectDir, stageCode, tc, "output"))
	}
	dirs = append(dirs, ws.StageStateDir(localProjectDir, stageCode, "output")) // 兼容遗留环节级
	seen := map[string]bool{}
	var files []string
	for _, d := range dirs {
		entries, err := os.ReadDir(d)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			p := filepath.Join(d, e.Name())
			if fi, ferr := e.Info(); ferr == nil && isPlaceholderFile(p, fi.Size()) {
				continue
			}
			if seen[e.Name()] {
				continue
			}
			seen[e.Name()] = true
			files = append(files, p)
		}
	}
	return files
}

// UploadCentralizedStageFinals 把某环节定稿上传到 manage（键=projectKey 项目级唯一），供跨机下游拉取。
// localProjectDir：本机项目目录名(centralizedDirCode)；projectKey：项目级唯一键(project_code)。
// 返回成功数与逐文件错误。失败由调用方决定是否阻断（CPA 处用非阻断）。
func UploadCentralizedStageFinals(db *sqlx.DB, client *http.Client, endpoint, localProjectDir, projectKey, stageCode, uploadedBy string) (int, []string) {
	endpoint = strings.TrimRight(endpoint, "/")
	if endpoint == "" {
		return 0, []string{"未配置 manage_endpoint"}
	}
	if projectKey == "" {
		return 0, []string{"缺少项目编码(project_code)，无法跨机上传定稿"}
	}
	client = defaultHTTPClient(client)
	ws := NewProjectWorkspace(NewSystemConfigRepository(db).GetEffectiveProjectRoot())
	files := collectCentralizedStageOutputs(ws, localProjectDir, stageCode)
	uploaded := 0
	var errs []string
	for _, p := range files {
		if err := uploadOneFinal(client, endpoint, projectKey, stageCode, filepath.Base(p), uploadedBy, p); err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", filepath.Base(p), err))
			continue
		}
		uploaded++
	}
	return uploaded, errs
}

// PullCentralizedStageFinals 按 (projectKey, fromStageCode) 从 manage 拉定稿，落到本机下游环节 intoStageCode 的 input/。
// 返回拉取的文件名。无定稿时返回 (nil,nil)。
func PullCentralizedStageFinals(db *sqlx.DB, client *http.Client, endpoint, projectKey, fromStageCode, localProjectDir, intoStageCode string) ([]string, error) {
	endpoint = strings.TrimRight(endpoint, "/")
	if endpoint == "" {
		return nil, fmt.Errorf("未配置 manage_endpoint")
	}
	if projectKey == "" || fromStageCode == "" {
		return nil, nil
	}
	client = defaultHTTPClient(client)
	resp, err := client.Get(fmt.Sprintf("%s/api/projects/stage-finals-list?template_code=%s&stage_code=%s",
		endpoint, url.QueryEscape(projectKey), url.QueryEscape(fromStageCode)))
	if err != nil {
		return nil, fmt.Errorf("取上游定稿清单失败: %w", err)
	}
	var listResp struct {
		Code int `json:"code"`
		Data []struct {
			ID       int64  `json:"id"`
			FileName string `json:"file_name"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&listResp); err != nil {
		resp.Body.Close()
		return nil, fmt.Errorf("解析上游清单失败: %w", err)
	}
	resp.Body.Close()
	if listResp.Code != 0 || len(listResp.Data) == 0 {
		return nil, nil
	}
	inputDir := NewProjectWorkspace(NewSystemConfigRepository(db).GetEffectiveProjectRoot()).StageStateDir(localProjectDir, intoStageCode, "input")
	if err := os.MkdirAll(inputDir, 0o755); err != nil {
		return nil, fmt.Errorf("建 input 目录失败: %w", err)
	}
	var pulled []string
	for _, f := range listResp.Data {
		if err := downloadOneFinal(client, endpoint, f.ID, filepath.Join(inputDir, sanitizeFileName(f.FileName))); err != nil {
			continue
		}
		pulled = append(pulled, f.FileName)
	}
	return pulled, nil
}
