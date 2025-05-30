package commands

import (
	userEntities "clean-arch-gin/internal/domain/user/entities"
	userRepositories "clean-arch-gin/internal/domain/user/repositories"
)

// CreateUserCommand represents a command to create a new user
type CreateUserCommand struct {
	Email    string
	Name     string
	Password string
}

// CreateUserCommandHandler handles CreateUserCommand
type CreateUserCommandHandler struct {
	userRepo userRepositories.UserRepository
	// eventBus EventBus // For publishing domain events
}

// NewCreateUserCommandHandler creates a new command handler
func NewCreateUserCommandHandler(userRepo userRepositories.UserRepository) *CreateUserCommandHandler {
	return &CreateUserCommandHandler{
		userRepo: userRepo,
	}
}

// Handle executes the create user command
func (h *CreateUserCommandHandler) Handle(cmd CreateUserCommand) (*userEntities.User, error) {
	// Business logic validation
	if err := h.validateCommand(cmd); err != nil {
		return nil, err
	}

	// Check if user already exists
	_, err := h.userRepo.GetByEmail(cmd.Email)
	if err == nil {
		return nil, userEntities.ErrEmailExists
	}
	if err != userEntities.ErrUserNotFound {
		return nil, err
	}

	// Create domain entity using factory method
	user, err := userEntities.NewUser(cmd.Email, cmd.Name, cmd.Password)
	if err != nil {
		return nil, err
	}

	// Persist the user
	if err := h.userRepo.Create(user); err != nil {
		return nil, err
	}

	// Publish domain event (if using event-driven architecture)
	// h.eventBus.Publish(events.UserCreatedEvent{
	//     UserID: user.ID,
	//     Email:  user.Email,
	//     Name:   user.Name,
	// })

	return user, nil
}

// validateCommand performs command-specific validation
func (h *CreateUserCommandHandler) validateCommand(cmd CreateUserCommand) error {
	// Additional business rules can be validated here
	// that are specific to the command context

	if len(cmd.Password) < 8 {
		return userEntities.ErrInvalidPassword
	}

	// Email format validation could be added here
	// Domain-specific validation rules

	return nil
}
