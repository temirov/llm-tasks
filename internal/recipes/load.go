package recipes

import (
	"encoding/xml"
	"os"
)

func LoadFromFile(path string) (Recipe, error) {
	var recipe Recipe
	data, err := os.ReadFile(path)
	if err != nil {
		return recipe, err
	}
	if err := xml.Unmarshal(data, &recipe); err != nil {
		return recipe, err
	}
	return recipe, nil
}
