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

这个 MCP server 提供 3 个工具：

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

## 参数说明

- `filePath`: 必填，目标文件的绝对路径；不支持相对路径
- `rootUri`: 可选，workspace 根目录。支持 `file://...` 和普通本地路径；不传时自动搜索
- `line`: 对 `find_definition` / `find_references` 必填，1-based 行号
- `symbolName`: 对 `find_definition` / `find_references` 必填
- `index`: 可选，1-based，表示 `symbolName` 在这一行中是第几次出现；`0` 表示未指定

## 返回结构

`find_definition` / `find_references` 返回：

```json
{
  "items": [
    {
      "filePath": "/abs/path/to/file.go",
      "line": 3,
      "column": 6,
      "endLine": 3,
      "endColumn": 12
    }
  ],
  "warnings": []
}
```

`file_outline` 返回：

```json
{
  "items": [
    {
      "name": "MyType",
      "kind": "Class",
      "detail": "",
      "filePath": "/abs/path/to/file.go",
      "line": 10,
      "column": 6,
      "endLine": 10,
      "endColumn": 12,
      "containerName": ""
    }
  ]
}
```

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
