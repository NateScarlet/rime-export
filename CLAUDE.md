# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## 构建与测试

```bash
go build -o export_table.exe .   # 编译
go install .                      # 安装到 $GOPATH/bin
go test -v ./...                  # 运行所有测试
go test -v -run TestSpToQuanpin   # 运行单个测试
```

## 架构

这是 Rime table.bin → 岁寒输入法文本词库的导出工具。

**输入**: Rime 的 `table.bin` 二进制词典文件（`Rime::Table/4.0` 格式）
**输出**: 岁寒输入法支持的文本词条（`拼音 pin|yin 权重`，UTF-8 编码）

### 处理流程

1. **解析 Metadata**（68 字节头）— 获取 syllabary/index/string_table 偏移
2. **加载 String Table** — 用 `go-marisa` 解析 marisa trie，提供 StringId → string 映射
3. **读取 Syllabary** — int32 数组，每个元素是 string table 中的 StringId
4. **遍历 Index 树** — 4 层索引结构：`HeadIndex → TrunkIndex(lv2) → TrunkIndex(lv3) → TailIndex(lv4)`
5. **双拼→全拼转换** — 剥离 `[` 后的辅助码，按搜狗双拼布局解码
6. **输出** — 按权重降序写入 UTF-8 文本

### 关键数据结构（对应 librime 源码）

- `OffsetPtr<T>` — int32 偏移，相对于自身的地址
- `List<T>` — `{ uint32 size, OffsetPtr<T> at }`
- `Array<T>` — `{ uint32 size, T at[1] }`
- `HeadIndexNode` — `{ List<Entry>, OffsetPtr<PhraseIndex> }`
- `TrunkIndexNode` — `{ int32 key, List<Entry>, OffsetPtr<PhraseIndex> }`
- `LongEntry` — `{ List<SyllableId> extra_code, Entry entry }`

### 双拼编码

本工具面向 **搜狗双拼 + 墨奇音形** 方案（`main.schema.yaml`）。词典中的编码格式为 `双拼[辅助码`，只取 `[` 前的双拼部分转全拼。改动双拼映射时须保持搜狗布局一致性。

### 文件说明

- `main.go` — 主程序（二进制解析 + 索引遍历 + 双拼转换 + 输出）
- `export_test.go` — 双拼→全拼单元测试
- `integration_test.go` — 集成测试（直接读 table.bin 验证特定词条）
