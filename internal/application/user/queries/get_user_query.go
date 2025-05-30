package queries

import (
	userEntities "clean-arch-gin/internal/domain/user/entities"
	userRepositories "clean-arch-gin/internal/domain/user/repositories"
)

// GetUserQuery represents a query to get a user by ID
type GetUserQuery struct {
	UserID uint
}

// GetUserQueryHandler handles GetUserQuery
type GetUserQueryHandler struct {
	userRepo userRepositories.UserRepository
	// Could also use a read-only repository optimized for queries
	// userReadRepo ReadOnlyUserRepository
}

// NewGetUserQueryHandler creates a new query handler
func NewGetUserQueryHandler(userRepo userRepositories.UserRepository) *GetUserQueryHandler {
	return &GetUserQueryHandler{
		userRepo: userRepo,
	}
}

// Handle executes the get user query
func (h *GetUserQueryHandler) Handle(query GetUserQuery) (*userEntities.User, error) {
	if query.UserID == 0 {
		return nil, userEntities.ErrInvalidEmail // Reusing error for invalid ID
	}

	user, err := h.userRepo.GetByID(query.UserID)
	if err != nil {
		return nil, err
	}

	return user, nil
}

// GetUsersQuery represents a query to get multiple users with pagination
type GetUsersQuery struct {
	Limit  int
	Offset int
	// Additional filters could be added
	Email    string
	Status   string
	SortBy   string
	SortDesc bool
}

// GetUsersQueryHandler handles GetUsersQuery
type GetUsersQueryHandler struct {
	userRepo userRepositories.UserRepository
}

// NewGetUsersQueryHandler creates a new query handler
func NewGetUsersQueryHandler(userRepo userRepositories.UserRepository) *GetUsersQueryHandler {
	return &GetUsersQueryHandler{
		userRepo: userRepo,
	}
}

// Handle executes the get users query
func (h *GetUsersQueryHandler) Handle(query GetUsersQuery) ([]*userEntities.User, error) {
	// Apply default values
	if query.Limit <= 0 {
		query.Limit = 10
	}
	if query.Offset < 0 {
		query.Offset = 0
	}

	// In a real implementation, you might have more sophisticated
	// filtering and sorting capabilities
	users, err := h.userRepo.GetAll(query.Limit, query.Offset)
	if err != nil {
		return nil, err
	}

	return users, nil
}

// UserStatsQuery represents a query for user statistics
type UserStatsQuery struct {
	DateFrom string
	DateTo   string
}

// UserStatsResult represents the result of user statistics query
type UserStatsResult struct {
	TotalUsers   int64
	ActiveUsers  int64
	NewUsers     int64
	DeletedUsers int64
}

// GetUserStatsQueryHandler handles UserStatsQuery
type GetUserStatsQueryHandler struct {
	userRepo userRepositories.UserRepository
	// In real implementation, might use a specialized analytics repository
	// analyticsRepo AnalyticsRepository
}

// NewGetUserStatsQueryHandler creates a new query handler
func NewGetUserStatsQueryHandler(userRepo userRepositories.UserRepository) *GetUserStatsQueryHandler {
	return &GetUserStatsQueryHandler{
		userRepo: userRepo,
	}
}

// Handle executes the user stats query
func (h *GetUserStatsQueryHandler) Handle(query UserStatsQuery) (*UserStatsResult, error) {
	// In a real implementation, this would execute complex queries
	// potentially against read-optimized databases or data warehouses

	totalUsers, err := h.userRepo.Count()
	if err != nil {
		return nil, err
	}

	return &UserStatsResult{
		TotalUsers:   totalUsers,
		ActiveUsers:  totalUsers, // Placeholder
		NewUsers:     0,          // Placeholder
		DeletedUsers: 0,          // Placeholder
	}, nil
}
