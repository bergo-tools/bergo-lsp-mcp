# bergo-lsp-mcp

一个基于 Go 的 `stdio` MCP server，用来桥接本地 LSP server，并暴露 3 个 MCP tools：

- `find_references`
- `find_definition`
- `file_outline`

## Quick Start

直接安装到 `$GOBIN` / `$GOPATH/bin`：

```bash
go install github.com/bergo-tools/bergo-lsp-mcp@latest
bergo-lsp-mcp
```

直接运行：

```bash
go run .
```

或者先构建：

```bash
go build -o bergo-lsp-mcp .
./bergo-lsp-mcp
```

它是一个 `stdio` MCP server，通常应该由 MCP host 作为子进程拉起。

## MCP Host 配置

如果你的 MCP host 支持配置 `command` / `args`，可以这样接入：

```json
{
  "command": "go",
  "args": ["run", "."],
  "cwd": "/path/to/bergo-lsp-mcp"
}
```

或者使用已经构建好的二进制：

```json
{
  "command": "/path/to/bergo-lsp-mcp"
}
```

如果要指定配置文件路径：

```json
{
  "command": "/path/to/bergo-lsp-mcp",
  "env": {
    "BERGO_LSP_MCP_CONFIG": "/path/to/config.json"
  }
}
```

## 直接怎么用

这个 MCP server 提供 5 个工具：

### `find_definition`

```json
{
  "filePath": "/abs/path/to/file.go",
  "rootUri": "file:///abs/path/to/project",
  "line": 12,
  "symbolName": "helper",
  "index": 0
}
```

### `find_references`

```json
{
  "filePath": "/abs/path/to/file.go",
  "rootUri": "file:///abs/path/to/project",
  "line": 12,
  "symbolName": "helper",
  "index": 0
}
```

### `file_outline`

```json
{
  "filePath": "/abs/path/to/file.go",
  "rootUri": "file:///abs/path/to/project"
}
```

### `find_implementation`

```json
{
  "filePath": "/abs/path/to/file.go",
  "rootUri": "file:///abs/path/to/project",
  "line": 12,
  "symbolName": "helper",
  "index": 0
}
```

### `rename`

```json
{
  "filePath": "/abs/path/to/file.go",
  "rootUri": "file:///abs/path/to/project",
  "line": 12,
  "symbolName": "helper",
  "index": 0,
  "newName": "renamedHelper"
}
```

## 参数说明

- `filePath`: 必填，目标文件的绝对路径；不支持相对路径
- `rootUri`: 可选，workspace 根目录。支持 `file://...` 和普通本地路径；不传时自动搜索
- `line`: 对 `find_definition` / `find_references` 必填，1-based 行号
- `symbolName`: 对 `find_definition` / `find_references` / `find_implementation` / `rename` 必填
- `index`: 可选，1-based，表示 `symbolName` 在这一行中是第几次出现；`0` 表示未指定
- `newName`: 对 `rename` 必填，目标新名字；`rename` 会直接修改磁盘文件

## 返回结构

`find_definition` / `find_references` / `find_implementation` 返回：

```json
{
  "items": [
    "/abs/path/to/file.go:3: func helper() {}"
  ],
  "warnings": []
}
```

字段说明：

- `items`: 文本数组。每一项格式为 `filepath:line: lineText`
- `warnings`: 可选。服务端在做符号定位回退、结果过滤或结果校验不完全时附带的提示信息

`file_outline` 返回：

```json
{
  "filePath": "/abs/path/to/file.go",
  "items": [
    "class MyType [line 10-20]",
    "method MyType.Hello() error [line 12-15]"
  ],
  "warnings": []
}
```

字段说明：

- `filePath`: 当前 outline 对应的文件绝对路径，只返回一次
- `items`: 文本数组。每一项是一个符号摘要，包含类型、名称/签名和行号范围
- `warnings`: 预留字段，当前 `file_outline` 正常返回时通常为空或省略

`rename` 返回：

```json
{
  "items": [
    "/abs/path/to/file.go:3: func renamedHelper() {}",
    "/abs/path/to/file.go:6: renamedHelper()"
  ],
  "warnings": []
}
```

字段说明：

- `items`: 已修改结果数组。每一项是改名完成后受影响行的最新内容
- `warnings`: 可选。当前主要继承符号定位阶段的 warning

## 行为语义

### `find_definition`

- 使用 LSP 的 `textDocument/definition`
- 返回 `items[]`，不是单个对象
- 如果底层 LSP 返回多个 definition / `LocationLink`，会全部转换后返回，不会强行只保留一个
- 如果 LSP 返回 0 个位置，则返回空 `items`
- 服务会尝试用 `symbolName` 对 LSP 结果做二次校验；如果只能确认部分结果，会过滤掉不匹配项并在 `warnings` 里说明
- 如果完全无法可靠校验，但 LSP 已返回位置，则会保留原始 LSP 结果，并在 `warnings` 里说明

### `find_references`

- 使用 LSP 的 `textDocument/references`
- 当前请求参数里 `includeDeclaration=true`，所以结果通常包含声明 / 定义本身
- 是否一定包含声明，最终仍取决于底层语言服务器的实现
- 如果 LSP 返回 0 个位置，则返回空 `items`
- 和 `find_definition` 一样，服务会尝试按 `symbolName` 过滤或校验结果，并通过 `warnings` 暴露过滤/回退信息

### `find_implementation`

- 使用 LSP 的 `textDocument/implementation`
- 返回 `items[]`，不是单个对象
- 如果底层 LSP 返回多个 implementation / `LocationLink`，会全部转换后返回
- 如果 LSP 返回 0 个位置，则返回空 `items`
- 和 `find_definition` 一样，服务会尝试按 `symbolName` 过滤或校验结果，并通过 `warnings` 暴露过滤/回退信息

### `file_outline`

- 使用 LSP 的 `textDocument/documentSymbol`
- 返回扁平列表，不返回树形 JSON
- 如果底层 LSP 返回 `DocumentSymbol` 树，服务会递归拍平，并用 `containerName` 记录直接父级名称
- 如果底层 LSP 返回 `SymbolInformation`，则直接转成扁平项；此时层级信息取决于 LSP 是否提供 `containerName`
- 层级粒度完全取决于底层 LSP 暴露的 document symbols，通常会包含顶层声明，也可能包含方法、字段、内部符号等
- 如果 LSP 返回 0 个符号，则返回空 `items`

### `rename`

- 使用 LSP 的 `textDocument/rename`
- 会直接把 LSP 返回的重命名编辑写入磁盘文件
- 返回值里的 `items[]` 是改名完成后受影响行的最新内容
- 如果底层 LSP 返回 0 个编辑，则返回空 `items`，且不会修改文件
- `newName` 非法时，通常由底层 LSP 直接报错

## 失败语义

- 参数错误会直接报错，不返回空数组
- 典型参数错误包括：
  - `filePath` 不存在
  - `line < 1`
  - `symbolName` 为空
  - `index < 0`
  - `newName` 为空
  - 同一行里 `symbolName` 出现多次但没有提供可用的 `index`
  - `index` 超出该行匹配次数
- `symbolName` 在指定行找不到时，不会立刻报错；服务会回退到该行第一个非空白列继续请求 LSP，并在 `warnings` 里说明
- `rootUri` 不正确、语言识别失败、LSP 未安装、LSP 不支持对应 capability、LSP 请求失败时，tool 会直接返回错误
- 如果 LSP 成功返回但结果为空，tool 会返回空 `items`，而不是把“未找到”提升为错误
- 对于 “workspace root 不对” 或 “LSP 尚未索引完成” 这类情况，最终表现取决于底层 LSP：有些 LSP 会报错，有些会返回空结果。这个 server 不会把这两类情况再统一改写成固定语义

## `index` 规则

`find_definition` 和 `find_references` 里，`index` 用来解决一行里同一个 `symbolName` 出现多次的问题。

- `index=0` 或不传：表示未指定第几次
- 如果这一行只出现 1 次，直接使用这次出现
- 如果这一行出现多次且 `index=0`，服务会返回错误，提示调用方指定第几次出现
- `index=1` 表示第一次出现，`index=2` 表示第二次出现，以此类推

例子：

```go
helper(helper)
```

如果 `symbolName = "helper"`：

- `index=1` 选中左边那个
- `index=2` 选中括号里的那个
- `index=0` 会返回歧义错误

## `rootUri` 规则

根目录优先级如下：

1. MCP 调用方传入的 `rootUri`
2. 语言配置里的 `rootDirStrategy`
3. 语言配置里的 `rootMarkers`
4. 全局 `rootMarkers`
5. 如果都没找到，则回退到目标文件所在目录

`rootUri` 传入后，会同时影响：

- LSP `initialize` 里的 `rootUri`
- LSP 进程工作目录
- LSP client 的复用键

同一种语言但不同 `rootUri` 会启动不同的 LSP client。

## 不配配置文件也能跑

项目内置了一组常见语言的默认 LSP 配置。如果当前目录没有 `config.json`，会直接使用这些默认值：

- Go: `gopls`
- TypeScript / JavaScript: `typescript-language-server --stdio`
- Python: `pyright-langserver --stdio`
- Rust: `rust-analyzer`
- Lua: `lua-language-server`
- C / C++: `clangd`
- PHP: `intelephense --stdio`
- Ruby: `solargraph stdio`
- Bash: `bash-language-server start`
- YAML: `yaml-language-server --stdio`
- JSON: `vscode-json-language-server --stdio`

注意：这些命令需要你本机已经安装。

## 自定义配置

默认会尝试读取当前目录下的 `config.json`。如果不存在，就只使用内置默认配置。

也可以通过环境变量指定配置文件路径：

```bash
export BERGO_LSP_MCP_CONFIG=/path/to/config.json
```

配置文件结构示例见 [config.example.json](/Users/zp/Desktop/playground/bergo-lsp-mcp/config.example.json)。

示例：

```json
{
  "rootMarkers": [".git", "go.mod", "package.json"],
  "languages": [
    {
      "name": "go",
      "extensions": [".go"],
      "languageId": "go",
      "command": "gopls",
      "args": [],
      "env": {},
      "rootDirStrategy": "auto"
    },
    {
      "name": "markdown",
      "extensions": [".md"],
      "languageId": "markdown",
      "command": "marksman",
      "args": ["server"],
      "rootDirStrategy": "auto"
    }
  ]
}
```

规则：

- 如果 `languages[].name` 和内置语言同名，则覆盖该内置语言配置
- 如果是新的 `name`，则追加一门新语言
- `rootMarkers` 会和内置默认值合并

## 开发

测试：

```bash
go test ./...
```

构建：

```bash
go build ./...
```

项目里包含一个可选的 `gopls` 集成测试；如果本机没有安装 `gopls`，该测试会自动跳过。

## 实现说明

- 查询使用标准 LSP：
  - `textDocument/references`
  - `textDocument/definition`
  - `textDocument/documentSymbol`
- 定义结果兼容 `Location` 和 `LocationLink`
- 文件大纲统一返回扁平列表
- 查询前会把磁盘上的最新文件内容同步到 LSP
- 当前只处理本地磁盘文件，不处理未保存编辑缓冲区
