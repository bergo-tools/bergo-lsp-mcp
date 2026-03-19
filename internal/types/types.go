package types

type Position struct {
	FilePath  string `json:"filePath"`
	Line      int    `json:"line"`
	Column    int    `json:"column"`
	EndLine   int    `json:"endLine"`
	EndColumn int    `json:"endColumn"`
}

type QueryResult struct {
	Items    []Position `json:"items"`
	Warnings []string   `json:"warnings,omitempty"`
}

type OutlineItem struct {
	Name          string `json:"name"`
	Kind          string `json:"kind"`
	Detail        string `json:"detail,omitempty"`
	FilePath      string `json:"filePath"`
	Line          int    `json:"line"`
	Column        int    `json:"column"`
	EndLine       int    `json:"endLine"`
	EndColumn     int    `json:"endColumn"`
	ContainerName string `json:"containerName,omitempty"`
}

type OutlineResult struct {
	Items    []OutlineItem `json:"items"`
	Warnings []string      `json:"warnings,omitempty"`
}
