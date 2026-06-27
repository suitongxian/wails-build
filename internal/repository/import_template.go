package repository

import (
	"encoding/json"
	"fmt"
	"strings"
)

// 模版导入（2026-06-15）：把一棵五层模版树（JSON）在本地重建为可编辑模版（origin=local，新 template_code），
// 供用户从外部上传 / 粘贴 JSON 导入到本地模板库。
//
// 容错兼容多种来源的 JSON 形状（stages 嵌套在 template 内 或 与 template 同级，均可）：
//
//	A. scan GET /templates/:id/tree：{ "template": {...}, "stages": [...] }      （stages 同级）
//	B. 模版平台「模版导出」：{ "version","type","template": { ..., "stages": [...] } }（stages 嵌套 + 外层包装）
//	C. 扁平 canonical：{ "template_name": "...", "stages": [...] }                （根即 template）
//
// file_rules.required 兼容布尔与 0/1 数字；allowed_file_types 兼容逗号串与 JSON 数组串。

// ImportLocalTemplateFromJSON 解析五层模版树 JSON 并在本地重建为可编辑模版，返回新本地模版 id。
func (r *TemplateAuthoringRepository) ImportLocalTemplateFromJSON(raw []byte) (int64, error) {
	if len(strings.TrimSpace(string(raw))) == 0 {
		return 0, fmt.Errorf("导入内容为空")
	}
	var root map[string]any
	if err := json.Unmarshal(raw, &root); err != nil {
		return 0, fmt.Errorf("JSON 解析失败，请检查格式: %w", err)
	}
	// 定位 template 对象：优先外层包装的 template 子对象，否则根本身即 template。
	tplObj := root
	if t, ok := root["template"].(map[string]any); ok {
		tplObj = t
	}
	// 定位 stages：优先 template 内的 stages，回退到根上的同级 stages。
	stagesAny := tplObj["stages"]
	if stagesAny == nil {
		stagesAny = root["stages"]
	}

	tb, _ := json.Marshal(tplObj)
	var tpl maCloneTemplate
	if err := json.Unmarshal(tb, &tpl); err != nil {
		return 0, fmt.Errorf("模版信息解析失败: %w", err)
	}
	sb, _ := json.Marshal(stagesAny)
	var stages []maCloneStage
	if err := json.Unmarshal(sb, &stages); err != nil {
		return 0, fmt.Errorf("工作环节解析失败: %w", err)
	}

	if strings.TrimSpace(tpl.TemplateName) == "" {
		return 0, fmt.Errorf("缺少 template_name（模版名称）")
	}
	if len(stages) == 0 {
		return 0, fmt.Errorf("模版至少需要一个工作环节（stages）")
	}
	return r.rebuildClone(tpl, stages, true)
}
