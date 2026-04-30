# Blades 文档

本目录包含 Blades 项目的所有技术文档。

## 快速导航

- [文档索引](./INDEX.md) - 所有文档的快速导航
- [开发规范](./DEVELOPMENT_GUIDE.md) - AI Coding 开发范式和工作流程
- [文档模板](./templates/) - 各类文档模板

## 文档分类

| 类型 | 前缀 | 说明 |
|------|------|------|
| 功能文档 | `feature-` | 简单功能增强的说明 |
| 设计文档 | `design-` | 复杂功能的设计方案 |
| 参考文档 | `reference-` | 借鉴外部项目的设计 |
| 决策文档 | `decision-` | 技术决策记录 (ADR) |
| 架构文档 | `architecture-` | 系统架构设计 |

## 如何使用

### 创建新文档

1. 根据任务类型选择合适的模板（在 `templates/` 目录下）
2. 复制模板并重命名为 `{type}-{description}.md`
3. 填写 frontmatter 元数据
4. 编写文档内容
5. 将文档添加到 `INDEX.md`

### 示例

```bash
# 创建设计文档
cp templates/design-template.md design-my-feature.md

# 编辑文档
vim design-my-feature.md

# 添加到索引
# 编辑 INDEX.md，在相应分类下添加链接
```

## 文档规范

所有文档必须包含 frontmatter 元数据，其中必填字段为 `type`、`title`、`date`、`status`；`author`、`related`、`tags` 为可选字段：

```yaml
---
type: design      # 必填
title: 文档标题    # 必填
date: 2024-11-16 # 必填
status: draft    # 必填
author: 作者名    # 可选
related: []      # 可选
tags: []         # 可选
---
```

编写文档时请至少填写必填字段；如有作者、关联文档或标签信息，建议一并补充。

详细规范请参考 [开发规范](./DEVELOPMENT_GUIDE.md)。
