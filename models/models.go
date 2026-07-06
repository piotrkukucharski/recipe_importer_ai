package models

type Recipe struct {
	Name           string     `json:"name"`
	Description    string     `json:"description"`
	WorkingTime    int        `json:"working_time"`
	WaitingTime    int        `json:"waiting_time"`
	Servings       int        `json:"servings"`
	SourceURL      string     `json:"source_url"`
	ImageURL       string     `json:"image_url"`
	Keywords       []string   `json:"keywords"`
	Steps          []Step     `json:"steps"`
	DishImageIndex *int       `json:"dish_image_index,omitempty"`
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

func GetRecipeID(recipe map[string]interface{}) int {
	if idVal, exists := recipe["id"]; exists {
		if idFloat, ok := idVal.(float64); ok {
			return int(idFloat)
		} else if idInt, ok := idVal.(int); ok {
			return idInt
		}
	}
	return 0
}
