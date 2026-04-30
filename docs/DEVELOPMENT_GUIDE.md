# Blades AI Coding 开发规范

## 概述

本文档定义了 Blades 项目的 AI Coding 开发范式，旨在通过结构化的文档管理和清晰的工作流程，帮助 AI 更好地理解项目上下文，提高开发效率和代码质量。

## 核心理念

- **文档驱动开发**: 重要的设计决策和技术方案都应该有文档记录
- **AI 友好**: 文档结构标准化，便于 AI 理解和生成
- **渐进式复杂度**: 根据任务复杂度选择合适的开发流程
- **知识积累**: 通过文档积累项目知识，降低上下文理解成本

## 开发范式分类

### 1. 简单任务 (Simple Task)

**适用场景:**
- Bug 修复
- 小功能增强（< 500 行代码变更）
- 代码重构
- 文档更新

**工作流程:**
```
需求理解 → 对话澄清 → Plan Mode → 实现 → 测试 → (可选) 创建 feature-xxx.md
```

**何时创建文档:**
- 功能有一定复杂度，需要说明设计思路
- 涉及多个模块的协作
- 未来可能需要扩展或修改

**文档位置:** `docs/feature-xxx.md`

---

### 2. 复杂任务 (Complex Task)

**适用场景:**
- 新模块开发
- 架构级改动
- 核心功能重构
- 性能优化方案
- 复杂的业务逻辑

**工作流程:**
```
需求分析 → AI 生成设计文档草稿 → Review & 讨论 → 修改设计文档 
→ 确认 design-xxx.md → 实现 → 测试 → 更新文档（如有变更）
```

**设计文档要求:**
- 必须包含背景、目标、方案对比、API 设计
- 需要考虑兼容性、性能、可维护性
- Review 通过后才能开始实现

**文档位置:** `docs/design-xxx.md`

---

### 3. 参考任务 (Reference Task)

**适用场景:**
- 借鉴其他项目的优秀设计
- 实现业界标准或最佳实践
- 移植成熟的解决方案

**工作流程:**
```
确定参考对象 → AI 读取外部设计 → 分析适配性 → 调整方案 
→ 创建 reference-xxx.md → 实现 → 测试
```

**参考文档要求:**
- 说明参考来源（项目、文档链接）
- 对比原设计和调整后的方案
- 说明为什么需要调整

**文档位置:** `docs/reference-xxx.md`

---

### 4. 技术决策 (Technical Decision)

**适用场景:**
- 技术选型（库、框架、工具）
- 架构模式选择
- 重要的工程实践决策

**工作流程:**
```
识别决策点 → 列出备选方案 → 分析优劣 → 做出决策 → 记录 decision-xxx.md
```

**决策文档要求:**
- 使用 ADR (Architecture Decision Record) 格式
- 记录决策背景、备选方案、决策结果、后果
- 决策一旦做出，不轻易修改（除非有重大变化）

**文档位置:** `docs/decision-xxx.md`

---

### 5. 架构演进 (Architecture Evolution)

**适用场景:**
- 系统架构设计
- 模块划分调整
- 重大重构规划

**工作流程:**
```
现状分析 → 问题识别 → 目标架构设计 → 演进路径规划 
→ 创建 architecture-xxx.md → 分阶段实施
```

**架构文档要求:**
- 包含现状、问题、目标架构、演进路径
- 需要有架构图（可使用 Mermaid）
- 考虑向后兼容和平滑迁移

**文档位置:** `docs/architecture-xxx.md`

## 文档元数据规范

所有文档都应该包含统一的 frontmatter 元数据，便于 AI 快速理解文档内容和关联关系：

```yaml
---
type: feature|design|reference|decision|architecture
title: 文档标题
date: YYYY-MM-DD
status: draft|review|approved|implemented|deprecated
author: 作者名
related: [相关文档列表]
tags: [标签1, 标签2]
---
```

**字段说明:**
- `type`: 文档类型（必填）
- `title`: 文档标题（必填）
- `date`: 创建日期（必填）
- `status`: 文档状态（必填）
  - `draft`: 草稿
  - `review`: 审查中
  - `approved`: 已批准
  - `implemented`: 已实现
  - `deprecated`: 已废弃
- `author`: 作者（可选）
- `related`: 相关文档（可选，用于建立文档间关联）
- `tags`: 标签（可选，用于分类和检索）

## 文档命名规范

- 使用小写字母和连字符
- 格式: `{type}-{brief-description}.md`
- 示例:
  - `feature-streaming-support.md`
  - `design-memory-optimization.md`
  - `reference-langchain-tools.md`
  - `decision-use-generics.md`
  - `architecture-plugin-system.md`

## AI 协作最佳实践

### 1. 明确任务类型
在开始任务前，先判断任务类型，选择合适的工作流程。

### 2. 充分利用文档
- 实现前先查阅相关文档
- 参考 `docs/INDEX.md` 快速定位相关文档
- 注意文档的 `related` 字段，了解依赖关系

### 3. 及时更新文档
- 实现完成后，更新文档状态为 `implemented`
- 如果实现与设计有偏差，及时更新设计文档
- 废弃的方案标记为 `deprecated`，说明原因

### 4. 保持文档简洁
- 避免过度文档化
- 代码能说明的，不需要在文档中重复
- 关注"为什么"而不是"是什么"

### 5. 使用模板
- 所有文档都应该基于 `docs/templates/` 中的模板创建
- 模板确保文档结构一致，便于 AI 理解

## 文档审查清单

在提交文档前，请确认：

- [ ] 包含完整的 frontmatter 元数据
- [ ] 遵循命名规范
- [ ] 使用了正确的模板结构
- [ ] 相关文档已在 `related` 字段中列出
- [ ] 已添加到 `docs/INDEX.md`
- [ ] 技术术语使用准确
- [ ] 代码示例可以运行
- [ ] 图表清晰易懂（如有）

## 参考资源

- [文档索引](./INDEX.md) - 查找所有文档
- [文档模板](./templates/) - 各类文档模板
- [Architecture Decision Records](https://adr.github.io/) - ADR 最佳实践
