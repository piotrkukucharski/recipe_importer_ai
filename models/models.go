package models

type Recipe struct {
	Name         string     `json:"name"`
	Description  string     `json:"description"`
	WorkingTime  int        `json:"working_time"`
	WaitingTime  int        `json:"waiting_time"`
	Servings     int        `json:"servings"`
	SourceURL    string     `json:"source_url"`
	ImageURL     string     `json:"image_url"`
	Keywords     []string   `json:"keywords"`
	Steps        []Step     `json:"steps"`
}

type Step struct {
	Name        string       `json:"name"`
	Instruction string       `json:"instruction"`
	Ingredients []Ingredient `json:"ingredients"`
}

type Ingredient struct {
	Food   Food    `json:"food"`
	Unit   Unit    `json:"unit"`
	Amount float64 `json:"amount"`
	Note   string  `json:"note"`
}

type Food struct {
	Name string `json:"name"`
}

type Unit struct {
	Name string `json:"name"`
}
