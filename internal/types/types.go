package types

type QueryResult struct {
	Items    []string `json:"items"`
	Warnings []string `json:"warnings,omitempty"`
}

type RenameResult struct {
	Items    []string `json:"items"`
	Warnings []string `json:"warnings,omitempty"`
}

type OutlineResult struct {
	FilePath string   `json:"filePath"`
	Items    []string `json:"items"`
	Warnings []string `json:"warnings,omitempty"`
}
