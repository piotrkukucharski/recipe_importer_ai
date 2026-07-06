package tests

import (
	"context"
	"recipe_importer_ai/infrastructure/api"
	"recipe_importer_ai/infrastructure/apify"
	"recipe_importer_ai/infrastructure/gemini"
	"recipe_importer_ai/infrastructure/tandoor"
	"recipe_importer_ai/usecases/auth"
	"recipe_importer_ai/usecases/cookbook"
	"recipe_importer_ai/usecases/duplicates"
	"recipe_importer_ai/usecases/import_recipe"
	"recipe_importer_ai/usecases/recipe"
)

func setupTestHandler(ctx context.Context) (*api.ApiHandler, error) {
	apifyClient := apify.NewApifyService()
	geminiClient, err := gemini.NewGeminiService(ctx)
	if err != nil {
		return nil, err
	}
	tandoorClient := tandoor.NewTandoorService()

	authUC := auth.NewAuthUseCase(tandoorClient)
	findDuplicatesUC := duplicates.NewFindUseCase(tandoorClient)
	cleanDuplicatesUC := duplicates.NewCleanUseCase(tandoorClient)

	tagsUC := cookbook.NewTagsUseCase(tandoorClient, geminiClient)
	matchUC := cookbook.NewMatchUseCase(geminiClient)
	suggestUC := cookbook.NewSuggestUseCase(tandoorClient, tagsUC, matchUC)
	addRecipesUC := cookbook.NewAddUseCase(tandoorClient)

	processor := import_recipe.NewProcessor(geminiClient)
	taskManager := import_recipe.NewTaskManager()
	importURLUC := import_recipe.NewImportURLUseCase(apifyClient, processor, tandoorClient, taskManager)
	importTextUC := import_recipe.NewImportTextUseCase(processor, tandoorClient, taskManager)
	importImageUC := import_recipe.NewImportImageUseCase(processor, tandoorClient, taskManager)
	recipeDeleteUC := recipe.NewDeleteUseCase(tandoorClient)

	h := api.NewApiHandler(
		tandoorClient,
		authUC,
		findDuplicatesUC,
		cleanDuplicatesUC,
		suggestUC,
		addRecipesUC,
		importURLUC,
		importTextUC,
		importImageUC,
		taskManager,
		recipeDeleteUC,
	)

	return h, nil
}
