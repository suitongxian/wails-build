#!/usr/bin/env node
/**
 * 书目印刷数据业务模版 —— 五层种子数据灌入脚本
 *
 * 用法（需先启动 scan，后端 HTTP 默认 :3001）：
 *   node scripts/seed-print-template.mjs [baseURL]
 *   例：node scripts/seed-print-template.mjs http://127.0.0.1:3001
 *
 * 行为：建「出版印刷」行业 → 建《明朝那些事儿》印刷计划项目模版 →
 *       按印刷流程建 10 个工作事项，每个事项下建文件任务，任务下建文档标识。
 * 各级编码（IND/TPL-LOCAL/STG/TK/IN-PRC-OUT）由 scan 后端自动生成，脚本只发业务字段。
 *
 * 注意：脚本不做去重，重复运行会再建一套（项目编码不同）。仅作演示/验收用。
 */

const BASE = (process.argv[2] || 'http://127.0.0.1:3001').replace(/\/$/, '')

async function post(path, body) {
  const res = await fetch(BASE + path, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })
  const json = await res.json().catch(() => ({}))
  if (!json.success) throw new Error(`POST ${path} 失败: ${json.error || res.status}`)
  return json.data
}

// ---- 书目印刷模版的五层数据（设计文档 §6.1 + §13，补出「文件任务」中间层）----
const TEMPLATE = {
  businessClass: { name: '出版印刷', description: '图书/期刊印刷数据业务' },
  project: {
    scope: 'industry',
    template_name: '《明朝那些事儿》印刷计划',
    short_code: 'MC-NSXS',
    manager: '刘老师',
    owner: '第一研究院',
    sensitivity_level: 'core',
    approval_basis: '出版计划批文',
    description: '书目印刷数据业务模版示例（演示用）',
  },
  stages: [
    {
      name: '收稿登记', manager: '刘老师', members: '王老师',
      tasks: [
        {
          name: '客户材料接收', manager: '刘老师', sensitivity_level: 'general',
          rules: [
            { file_name: '客户原稿', data_state: 'input', required: true, allowed_file_types: 'PDF,DOC,DOCX,JPG', naming_pattern: '{书名}-原稿', drafter: '客户' },
            { file_name: '客户委托书', data_state: 'input', required: true, allowed_file_types: 'PDF', naming_pattern: '{书名}-委托书', drafter: '客户' },
          ],
        },
        {
          name: '收稿凭证开具', manager: '刘老师', sensitivity_level: 'general',
          rules: [
            { file_name: '收稿凭证', data_state: 'output', required: true, allowed_file_types: 'PDF', naming_pattern: '{书名}-收稿凭证' },
          ],
        },
      ],
    },
    {
      name: '排版', manager: '赵编辑',
      tasks: [
        {
          name: '排版加工', manager: '赵编辑', sensitivity_level: 'important',
          rules: [
            { file_name: '原稿文件', data_state: 'input', required: true, allowed_file_types: 'PDF,DOC,DOCX', naming_pattern: '{书名}-原稿' },
            { file_name: '排版临时文件', data_state: 'process', required: false, allowed_file_types: 'INDD,AI,PSD', naming_pattern: '{书名}-排版-V{版本}' },
            { file_name: '校对修改记录', data_state: 'process', required: false, allowed_file_types: 'PDF,DOCX', naming_pattern: '{书名}-校对记录-V{版本}' },
            { file_name: '排版完成稿', data_state: 'output', required: true, allowed_file_types: 'PDF', naming_pattern: '{书名}-排版定稿-V{版本}' },
          ],
        },
      ],
    },
    {
      name: '审校', manager: '钱专家',
      tasks: [
        {
          name: '专家审校', manager: '钱专家', sensitivity_level: 'core',
          rules: [
            { file_name: '审校申请', data_state: 'input', required: true, allowed_file_types: 'PDF', naming_pattern: '{书名}-审校申请' },
            { file_name: '排版完成稿', data_state: 'input', required: true, allowed_file_types: 'PDF', naming_pattern: '{书名}-排版定稿-V{版本}' },
            { file_name: '审校意见', data_state: 'output', required: true, allowed_file_types: 'PDF,DOCX', naming_pattern: '{书名}-审校意见' },
            { file_name: '修改确认稿', data_state: 'output', required: true, allowed_file_types: 'PDF', naming_pattern: '{书名}-修改确认稿' },
          ],
        },
      ],
    },
    {
      name: '封面设计', manager: '孙设计',
      tasks: [
        {
          name: '封面创作', manager: '孙设计', sensitivity_level: 'general',
          rules: [
            { file_name: '设计初稿', data_state: 'output', required: false, allowed_file_types: 'PSD,AI,JPG', naming_pattern: '{书名}-封面初稿-V{版本}' },
            { file_name: '封面定稿', data_state: 'output', required: true, allowed_file_types: 'PDF,AI', naming_pattern: '{书名}-封面定稿' },
          ],
        },
      ],
    },
    {
      name: '印刷', manager: '孙师傅',
      tasks: [
        {
          name: '印刷生产', manager: '孙师傅', sensitivity_level: 'important',
          rules: [
            { file_name: '印刷工艺单', data_state: 'input', required: true, allowed_file_types: 'PDF', naming_pattern: '{书名}-印刷工艺单' },
            { file_name: '输出印刷文件', data_state: 'input', required: true, allowed_file_types: 'PDF', naming_pattern: '{书名}-印刷文件' },
            { file_name: '印刷生产记录', data_state: 'process', required: false, allowed_file_types: 'PDF,XLSX', naming_pattern: '{书名}-印刷生产记录' },
            { file_name: '印刷成品样', data_state: 'output', required: true, allowed_file_types: 'JPG,PDF', naming_pattern: '{书名}-印刷成品样' },
          ],
        },
      ],
    },
    {
      name: '装订', manager: '周师傅',
      tasks: [
        {
          name: '装订生产', manager: '周师傅', sensitivity_level: 'important',
          rules: [
            { file_name: '装订工艺单', data_state: 'input', required: true, allowed_file_types: 'PDF', naming_pattern: '{书名}-装订工艺单' },
            { file_name: '装订过程记录', data_state: 'process', required: false, allowed_file_types: 'PDF,XLSX', naming_pattern: '{书名}-装订过程记录' },
            { file_name: '装订成品', data_state: 'output', required: true, allowed_file_types: 'JPG,PDF', naming_pattern: '{书名}-装订成品' },
          ],
        },
      ],
    },
    {
      name: '交付', manager: '刘老师',
      tasks: [
        {
          name: '成品交付', manager: '刘老师', sensitivity_level: 'general',
          rules: [
            { file_name: '印刷成品样', data_state: 'input', required: true, allowed_file_types: 'JPG,PDF', naming_pattern: '{书名}-印刷成品样' },
            { file_name: '交付清单', data_state: 'output', required: true, allowed_file_types: 'PDF,XLSX', naming_pattern: '{书名}-交付清单' },
            { file_name: '客户签收单', data_state: 'output', required: true, allowed_file_types: 'PDF', naming_pattern: '{书名}-客户签收单' },
            { file_name: '验收报告', data_state: 'output', required: true, allowed_file_types: 'PDF', naming_pattern: '{书名}-验收报告' },
          ],
        },
      ],
    },
    {
      name: '归档封存', manager: '档案员',
      tasks: [
        {
          name: '卷宗归档', manager: '档案员', sensitivity_level: 'core',
          rules: [
            { file_name: '归档清单', data_state: 'output', required: true, allowed_file_types: 'PDF,XLSX', naming_pattern: '{书名}-归档清单' },
            { file_name: '项目档案包', data_state: 'output', required: true, allowed_file_types: 'ZIP,7Z', naming_pattern: '{书名}-项目档案包' },
          ],
        },
      ],
    },
    {
      name: '授权回收', manager: '安全员',
      tasks: [
        {
          name: '权限回收', manager: '安全员', sensitivity_level: 'core',
          rules: [
            { file_name: '权限回收记录', data_state: 'output', required: true, allowed_file_types: 'PDF', naming_pattern: '{书名}-权限回收记录' },
          ],
        },
      ],
    },
    {
      name: '痕迹清除', manager: '安全员',
      tasks: [
        {
          name: '痕迹清除', manager: '安全员', sensitivity_level: 'core',
          rules: [
            { file_name: '痕迹清除日志', data_state: 'output', required: true, allowed_file_types: 'PDF,LOG', naming_pattern: '{书名}-痕迹清除日志' },
            { file_name: '操作记录审计报告', data_state: 'output', required: true, allowed_file_types: 'PDF', naming_pattern: '{书名}-操作审计报告' },
          ],
        },
      ],
    },
  ],
}

async function main() {
  console.log(`目标 scan 后端：${BASE}`)

  const bc = await post('/business-classes', TEMPLATE.businessClass)
  console.log(`✓ 行业分类：${bc.name}（${bc.code}）`)

  const tpl = await post('/templates', { class_code: bc.code, ...TEMPLATE.project })
  console.log(`✓ 项目模版：${tpl.template_name}（${tpl.template_code}）`)

  let stageN = 0, taskN = 0, ruleN = 0
  for (const st of TEMPLATE.stages) {
    const stage = await post('/template-stages', {
      template_id: tpl.id, name: st.name, manager: st.manager || '', members: st.members || '',
    })
    stageN++
    console.log(`  ✓ 事项 ${stage.stage_code} ${stage.stage_name}`)
    for (const tk of st.tasks) {
      const task = await post('/template-tasks', {
        stage_id: stage.id, name: tk.name, manager: tk.manager || '', sensitivity_level: tk.sensitivity_level || '',
      })
      taskN++
      console.log(`    ✓ 任务 ${task.task_code} ${task.task_name}`)
      for (const r of tk.rules) {
        const rule = await post('/template-file-rules', { task_id: task.id, ...r })
        ruleN++
        console.log(`      ✓ 标识 ${rule.file_rule_code} ${rule.file_name} [${rule.data_state}]`)
      }
    }
  }

  console.log(`\n完成：1 行业 / 1 项目模版 / ${stageN} 事项 / ${taskN} 任务 / ${ruleN} 文档标识`)
  console.log(`在 scan「数据业务服务 ▸ 数据项目模版」里点该项目的「编辑结构」即可查看整棵树。`)
}

main().catch((e) => {
  console.error('种子灌入失败：', e.message)
  process.exit(1)
})
