package auth

import (
	"context"
	"recipe_importer_ai/infrastructure/tandoor"
)

type AuthUseCase struct {
	Tandoor *tandoor.TandoorService
}

func NewAuthUseCase(t *tandoor.TandoorService) *AuthUseCase {
	return &AuthUseCase{Tandoor: t}
}

func (uc *AuthUseCase) Authenticate(ctx context.Context, username, password string) (string, error) {
	return uc.Tandoor.Authenticate(username, password)
}
