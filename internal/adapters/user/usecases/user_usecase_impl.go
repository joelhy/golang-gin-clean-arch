package usecases

import (
	userEntities "clean-arch-gin/internal/domain/user/entities"
	userRepositories "clean-arch-gin/internal/domain/user/repositories"
	userUsecases "clean-arch-gin/internal/domain/user/usecases"
)

// userUseCase implements the UserUseCase interface
type userUseCase struct {
	userRepo userRepositories.UserRepository
}

// NewUserUseCase creates a new user use case
func NewUserUseCase(userRepo userRepositories.UserRepository) userUsecases.UserUseCase {
	return &userUseCase{
		userRepo: userRepo,
	}
}

// CreateUser creates a new user
func (uc *userUseCase) CreateUser(email, name, password string) (*userEntities.User, error) {
	// Business logic validation
	if email == "" || name == "" || password == "" {
		return nil, userEntities.ErrInvalidEmail
	}

	// Check if user already exists
	_, err := uc.userRepo.GetByEmail(email)
	if err == nil {
		return nil, userEntities.ErrEmailExists
	}
	if err != userEntities.ErrUserNotFound {
		return nil, err
	}

	// Create domain entity
	user, err := userEntities.NewUser(email, name, password)
	if err != nil {
		return nil, err
	}

	// Persist user
	if err := uc.userRepo.Create(user); err != nil {
		return nil, err
	}

	return user, nil
}

// GetUser retrieves a user by ID
func (uc *userUseCase) GetUser(id uint) (*userEntities.User, error) {
	return uc.userRepo.GetByID(id)
}

// GetUsers retrieves all users with pagination
func (uc *userUseCase) GetUsers(limit, offset int) ([]*userEntities.User, error) {
	return uc.userRepo.GetAll(limit, offset)
}

// UpdateUser updates user information
func (uc *userUseCase) UpdateUser(id uint, email, name string) (*userEntities.User, error) {
	user, err := uc.userRepo.GetByID(id)
	if err != nil {
		return nil, err
	}

	user.UpdateInfo(name, email)

	if err := uc.userRepo.Update(user); err != nil {
		return nil, err
	}

	return user, nil
}

// DeleteUser soft deletes a user
func (uc *userUseCase) DeleteUser(id uint) error {
	return uc.userRepo.Delete(id)
}
