# CLAUDE.md — new-api 项目约定

## 强制要求：使用 Read 工具读取 AGENTS.md

不要将 `@AGENTS.md` 视为已加载。Claude Code 不会可靠地内联该引用。

在进行任何规划、编码、审查或回答项目问题之前，必须首先使用 Read 工具读取仓库根目录的 `AGENTS.md` 文件，并等待完整内容。这是每个会话以及每个新任务的第一步。

规则：

- 不要从记忆、摘要或本文件单独开始工作。
- 不要因为上一轮提到过 `AGENTS.md` 就跳过读取。
- 不要用 grep、glob 或部分查看代替完整 Read。
- 读取后，在后续工作中遵循 `AGENTS.md` 的所有规则。
- 如果任务涉及 `web/`，编辑前还必须读取 `web/AGENTS.md`。
