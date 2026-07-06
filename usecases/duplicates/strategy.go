package duplicates

type DuplicateGroup struct {
	Strategy string                   `json:"strategy"`
	Key      string                   `json:"key"`
	Recipes  []map[string]interface{} `json:"recipes"`
}
