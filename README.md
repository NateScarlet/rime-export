# rime-export

将 Rime 输入法的 `table.bin` 二进制词典导出为岁寒输入法可导入的文本词库。

## 安装

```bash
go install github.com/NateScarlet/rime-export
```

## 使用

```bash
export_table input/main.table.bin output/export.txt
```

不提供 `output.txt` 时输出到 stdout。

## 输出格式

```
拼音 pin|yin 权重
```

即岁寒输入法[格式3](docs/third_party/岁寒词库操作.md)，UTF-8 编码，按权重降序排列。

## 依赖

- Go 1.26+
- [go-marisa](https://github.com/pgaskin/go-marisa) — marisa trie 读取库

## 原理

程序直接解析 `table.bin` 的二进制结构：

1. 读取 Metadata 定位 syllabary、index tree 和 marisa string table
2. 用 marisa trie 解析字符串表
3. 遍历 4 层索引树（HeadIndex → TrunkIndex×2 → TailIndex）收集全部词条
4. 将搜狗双拼编码转为全拼输出

词典中的编码形如 `dj[kd/hg[xd`（丹恒），`[` 前是双拼，`[` 后是辅助码（被丢弃）。
