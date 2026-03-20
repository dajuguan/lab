# TEE Article Restructure Plan

## Summary

基于当前 `tee.md` 的最新框架，本文将采用如下章节顺序：

`TEE 定义 -> Threat Model and Security Principles -> 隔离机制 -> 运行时状态保护 -> 远程证明 -> TDX/Nitro 比较 -> References`

本轮修改的重点不再是保留大段 TDX 原文摘录，而是将第二节重写为“抽象主线 + TDX/Nitro 映射”的学术综述结构，并统一全文标题、段首句与结尾回扣的表达风格。

## Goals

- 将 `tee.md` 改写为一篇学术化 research summary，而不是资料摘抄式笔记。
- 保留当前大章节结构，不回退到旧版 6 节纯机制线性稿。
- 让第二节承担威胁模型与安全原则的抽象定义功能，为后续三节机制分析提供统一前提。
- 保证正文主论证仍以前三个 references 为核心，其余参考资料仅作补充。

## Planned Changes

### 1. TEE 的定义

- 删除当前中英混排且来源口径不一致的定义拼接。
- 改写为单一学术定义段：
  先定义 TEE 的一般含义，再说明术语不统一的现象，最后给出本文采用的统一分类口径。
- 保留 `VM-based TEE` 与 `enclave-based TEE` 作为两类常见实现形态，但不在本节展开具体机制。

### 2. Threat Model and Security Principles

- 将当前大段 TDX 英文原文全部改写为中文学术归纳，不保留整段英文摘录。
- 本节内部按两部分组织：
  - `Threat Model`：定义 adversary 范围、DoS 不在保障范围、TCB 收缩目标。
  - `Security Principles`：定义 confidentiality、integrity、CPU state protection、I/O boundary 等基本安全原则。
- 在抽象定义后，分别用一段说明 TDX 和 Nitro Enclaves 如何映射到这一框架。
- 本节只解释“为何需要这些原则”，不提前替代后续机制章节。

### 3. 隔离机制

- 保留“隔离机制”作为独立一节。
- 与第二节分工明确：
  第二节说明 threat model 下隔离为何必要；
  第三节说明隔离如何在 TDX 与 Nitro 中具体落地。
- 重点讨论：
  受保护边界、宿主控制权削弱、TCB 收缩、VM 边界与 enclave 边界的差异。

### 4. 运行时状态保护

- 将本节明确组织为四个概念：
  `memory confidentiality`、`integrity protection`、`CPU state protection`、`I/O boundary`。
- TDX 负责说明硬件支持下的内存与状态保护语义。
- Nitro Enclaves 负责说明云平台收缩接口面后，对工作负载与 I/O 模型带来的后果。
- 保证本节聚焦“运行时状态保护”，不与远程证明混写。

### 5. 远程证明

- 保持两层结构：
  `initial measurement` 解释启动身份，
  `attestation evidence` 解释平台与执行身份的可验证性。
- 避免使用 `execution proof` 作为正文主术语。
- 分别说明 TDX 与 Nitro 的 attestation 证明对象和信任链落点。

### 6. TDX 与 Nitro Enclaves 的比较分析

- 固定按四个维度回收前文结论：
  `隔离粒度`、`信任基底`、`资源模型`、`attestation 落点`。
- 不在本节引入新的机制定义。
- 结论目标是强调两者属于同一问题空间中的不同工程实现，而非互斥概念体系。

## Acceptance Checks

- `tee.md` 保持当前新框架，不回退到旧结构。
- 第二节不再出现大段英文原文摘录。
- 全文以中文学术综述为主，仅保留必要英文术语。
- 第二节负责抽象框架，第 3-5 节负责机制展开，章节之间分工清晰。
- 正文关键论断可回溯到前三个 references。
- 第六节只做总结性比较，不新增未定义机制。
- 标题与段首句均为定义式、分析式表达，而非口语化提问式表达。
