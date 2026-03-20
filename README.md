# bergo-lsp-mcp

一个基于 Go 的 `stdio` MCP server，用来桥接本地 LSP server，提供面向 LLM 的语义代码导航和重命名能力。

当前暴露 5 个 MCP tools：

- `find_references`
- `find_definition`
- `find_implementation`
- `file_outline`
- `rename`

设计目标：

- 优先返回适合 LLM 直接阅读的文本结果，而不是细粒度坐标对象
- 使用本地 LSP 提供 definition / references / implementation / rename
- `rename` 会直接修改磁盘文件，并返回修改后的行内容

## Quick Start

直接安装到 `$GOBIN` / `$GOPATH/bin`：

```bash
go install github.com/bergo-tools/bergo-lsp-mcp@latest
bergo-lsp-mcp
```

它是一个 `stdio` MCP server，通常应该由 MCP host 作为子进程拉起，不需要手动运行。

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

配置好后最好在你的Agent的Rules中添加一个说明来引导AI调用：


```
如果bergo-lsp插件可用，在查找某个符号的定义,引用和实现或批量改名某个符号，优先用这个插件提供的方法

```

## 工具详情

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
- `line`: 对 `find_definition` / `find_references` / `find_implementation` / `rename` 必填，1-based 行号
- `symbolName`: 对 `find_definition` / `find_references` / `find_implementation` / `rename` 必填
- `index`: 可选，1-based，表示 `symbolName` 在这一行中是第几次出现；`0` 表示未指定
- `newName`: 对 `rename` 必填，目标新名字；`rename` 会直接修改磁盘文件

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

## 默认配置

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
